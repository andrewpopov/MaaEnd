package innkeeping

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &ReactiveAction{}

// ReactiveAction supplies one verified next control to Pipeline. It deliberately
// uses optional marker navigation while Pipeline owns setup and control flow.
type ReactiveAction struct {
	mu       sync.Mutex
	sessions map[int64]*liveSession
}
type cookJob struct {
	ID              string    `json:"id"`
	Dish            string    `json:"dish"`
	Kitchen         string    `json:"kitchen"`
	Phase           string    `json:"phase"`
	ExpectedAt      time.Time `json:"expected_finish_at"`
	QueuedAt        time.Time `json:"queued_at"`
	StartEstimate   time.Time `json:"estimated_start_at"`
	CookSeconds     float64   `json:"cook_seconds"`
	CounterCheckDue bool      `json:"counter_check_due,omitempty"`
}
type pendingInput struct {
	Kind, Dish, Kitchen         string
	Seat                        string
	Gourmand                    bool
	GourmandReconciled          bool
	GourmandSlot, GourmandVisit int
	RequestedDish               string
	TrayBefore                  map[string]int
	Box                         [4]int
	IssuedAt                    time.Time
	BeforeSeconds               float64
	CookSeconds                 float64
	Before                      int
	Slot                        int
	At                          time.Time
}
type liveSession struct {
	TrayCapacity     int
	TrialRoutes      *trialRouteProfile
	ID               string
	Automatic        bool
	SetupAt          time.Time
	TravelAttempted  bool
	TravelPending    bool
	Navigation       navigationState
	Fixed            fixedRouteState
	UncertainSince   time.Time
	Started          bool
	SeenAt           time.Time
	Catalog          []DishDefinition
	Icons            IconLibrary
	Kitchens         map[string]ChefKitchen
	BatchIssued      map[string]int
	Jobs             []cookJob
	Tray             map[string]int
	Pending          *pendingInput
	Tracker          TimingTracker
	Visit            [6]int
	LastSeats        [6]string
	LastMessage      string
	CaptureRoot      string
	ServeTried       [5]bool
	Gourmands        map[string]*gourmandVisit
	Search           *cameraSearch
	Walk             *walkBurst
	InputHUD         *cameraSearch
	InputPhase       string
	InputVisual      *LiveView
	KitchenWaitUntil time.Time
}

func newLiveSession() (*liveSession, error) {
	if _, err := fsutil.StartupDir(); err != nil {
		return nil, err
	}
	catalog, err := LoadCatalog(fsutil.OutputPath("data", "Innkeeping", "recipes.json"))
	if err != nil {
		return nil, err
	}
	icons, err := LoadIcons(fsutil.OutputPath("data", "Innkeeping", "icons"))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fsutil.OutputPath("data", "Innkeeping", "reactive-layout.json"))
	if err != nil {
		return nil, err
	}
	var layout struct {
		CaptureRoot  string `json:"capture_root"`
		TrayCapacity int    `json:"tray_capacity"`
	}
	if err = json.Unmarshal(data, &layout); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(layout.CaptureRoot) {
		return nil, fmt.Errorf("capture_root must be an absolute image storage path")
	}
	routes, err := loadFixedRoutes()
	if err != nil {
		return nil, err
	}
	return &liveSession{Catalog: catalog, Icons: icons, Kitchens: map[string]ChefKitchen{}, Tray: map[string]int{}, CaptureRoot: layout.CaptureRoot, TrayCapacity: layout.TrayCapacity, Fixed: fixedRouteState{Routes: routes}}, nil
}
func (s *liveSession) announce(ctx *maa.Context, message string) {
	if message != s.LastMessage {
		maafocus.Print(ctx, message)
		s.LastMessage = message
	}
}
func route(ctx *maa.Context, arg *maa.CustomActionArg, node string) bool {
	return ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: node}}) == nil
}
func (s *liveSession) clickControl(ctx *maa.Context, arg *maa.CustomActionArg, box [4]int) bool {
	if s.Pending == nil || box[2] <= 0 || box[3] <= 0 {
		return false
	}
	s.Pending.Box = box
	if s.InputHUD != nil && time.Since(s.InputHUD.Started) <= 2*time.Second && (s.Pending.Kind == "pickup" || s.Pending.Kind == "serve" || s.Pending.Kind == "select_tray") {
		// The atomic action still captures and validates its control. These
		// already observed world controls do not need a second settling wait.
		return route(ctx, arg, "InnkeepingReactiveFastClick")
	}
	stabilityBox := box
	if s.Pending.Kind == "close" {
		// The close glyph is static while newly queued food is still animating.
		// Observe the queue itself before trying to dismiss its panel.
		stabilityBox = [4]int{100, 530, 320, 120}
	}
	// Wait for the actual control to settle, including right-side interaction
	// labels that lie outside the old fixed center-screen stability region.
	if err := ctx.OverridePipeline(map[string]any{
		"InnkeepingReactiveClick": map[string]any{
			"pre_wait_freezes": map[string]any{"target": stabilityBox, "time": 250, "threshold": .99, "timeout": 3000},
		},
	}); err != nil {
		return false
	}
	return route(ctx, arg, "InnkeepingReactiveClick")
}
func (s *liveSession) evidence(at time.Time, img image.Image, kind string) error {
	_, err := s.captureEvidence(at, img, kind)
	return err
}

func (s *liveSession) captureEvidence(at time.Time, img image.Image, kind string) (string, error) {
	if err := os.MkdirAll(s.CaptureRoot, 0755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(s.CaptureRoot, at.UTC().Format("20060102-150405")+"-"+kind+"-*.png")
	if err != nil {
		return "", err
	}
	// Evidence remains lossless; compression must not stall the service loop.
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(f, img); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}
func (s *liveSession) state(v LiveView) ReactiveState {
	return s.stateAt(v, time.Now())
}

func (s *liveSession) stateAt(v LiveView, now time.Time) ReactiveState {
	v.TrayCapacity = s.trayCapacity()
	v = s.gourmandView(v)
	state := ReactiveState{Phase: v.Phase, CurrentKitchen: v.Kitchen, Verified: s.Started, TrayCapacity: s.trayCapacity(), Tray: s.Tray, Ready: map[string]int{}, CounterPending: map[string]int{}, RemainingSeconds: float64(v.RemainingSeconds), Dishes: s.Catalog}
	state.CollectionDue = len(collectibleDishes(s.Jobs, v.Seats, now)) > 0
	s.reconcileBatchCounts()
	state.HoldKitchen = holdForBatch(s.Jobs, v, now)
	state.CurrentKitchenIdle = v.Phase == "kitchen" && v.QueueVerified && trayCount(v.Queue) == 0
	for _, seat := range v.Seats {
		if seat.Phase == "gourmand" {
			state.Demands = append(state.Demands, s.gourmandDemands(seat.Seat)...)
			continue
		}
		waiting := s.orderWaitingSeconds(seat.Seat, now)
		for slot, id := range seat.Dishes {
			state.Demands = append(state.Demands, Demand{ID: fmt.Sprintf("%s/%d", seat.Seat, slot), Seat: seat.Seat, Allowed: []string{id}, WaitingSeconds: waiting})
		}
	}
	for _, id := range []string{"1", "2"} {
		k := s.Kitchens[id]
		k.ID = id
		k.BatchIssued = s.BatchIssued[id]
		k.Capacity = 3
		k.Queued = map[string]int{}
		for _, j := range s.Jobs {
			if j.Kitchen == id {
				if j.Phase == "ready" {
					state.Ready[j.Dish]++
					continue
				}
				if j.CounterCheckDue {
					state.CounterPending[j.Dish]++
					continue
				}
				k.Queued[j.Dish]++
				if remaining := j.ExpectedAt.Sub(now).Seconds(); remaining > k.BusySeconds {
					k.BusySeconds = remaining
				}
			}
		}
		state.Kitchens = append(state.Kitchens, k)
	}
	return state
}

// orderWaitingSeconds is the observed lower bound for this seat's current visit.
// Missing frames preserve age; a turnover changes the visit identity and cannot
// inherit the previous diner's priority. Ordering time is not food-waiting time.
func (s *liveSession) orderWaitingSeconds(seat string, now time.Time) float64 {
	index := -1
	for i := 0; i < len(s.Visit); i++ {
		if seat == fmt.Sprint(i+1) {
			index = i
			break
		}
	}
	if index < 0 {
		return 0
	}
	key := fmt.Sprintf("diner:%s/seat%s-visit%d", s.ID, seat, s.Visit[index])
	track, ok := s.Tracker.tracks[key]
	if !ok || track.Observation.Phase != "waiting" || track.EnterBefore.IsZero() {
		return 0
	}
	return max(0, now.Sub(track.EnterBefore).Seconds())
}
func (s *liveSession) record(at time.Time, v LiveView) error {
	obs := []TimingObservation{}
	for i, seat := range v.Seats {
		phase := seat.Phase
		if phase == "unknown" {
			continue
		}
		if s.Visit[i] == 0 {
			s.Visit[i] = 1
		}
		previous := s.LastSeats[i]
		if (phase == "waiting" || phase == "ordering" || phase == "gourmand") && (previous == "dining" || previous == "bill_ready") {
			s.Visit[i]++
		}
		s.LastSeats[i] = phase
		obs = append(obs, TimingObservation{ID: fmt.Sprintf("%s/seat%s-visit%d", s.ID, seat.Seat, s.Visit[i]), Kind: "diner", Phase: phase, Seat: seat.Seat})
	}
	for _, j := range s.Jobs {
		if directlyObservedCook(j, v) {
			obs = append(obs, TimingObservation{ID: j.ID, Kind: "cook", Phase: j.Phase, Kitchen: j.Kitchen, Chef: s.Kitchens[j.Kitchen].Chef, Dish: j.Dish})
		}
	}
	events, err := s.Tracker.Observe(at, obs)
	if err != nil {
		return err
	}
	return AppendEvents(fsutil.OutputPath("data", "Innkeeping", "events.jsonl"), events)
}

// Run analyzes a fresh screenshot and sets a single next Pipeline action.
func (a *ReactiveAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var param struct {
		Reset        bool   `json:"reset"`
		Automatic    bool   `json:"automatic"`
		RouteID      string `json:"route_id"`
		TrialProfile string `json:"trial_profile"`
	}
	if arg.CustomActionParam != "" && json.Unmarshal([]byte(arg.CustomActionParam), &param) != nil {
		return false
	}
	if a.sessions == nil || param.Reset {
		a.sessions = map[int64]*liveSession{}
	}
	s := a.sessions[arg.TaskID]
	if s == nil {
		var err error
		s, err = newLiveSession()
		if err != nil {
			log.Error().Err(err).Msg("inn data load failed")
			return false
		}
		s.Automatic = param.Automatic
		s.ID = fmt.Sprintf("task%d-%d", arg.TaskID, time.Now().UnixNano())
		a.sessions[arg.TaskID] = s
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return false
	}
	if param.TrialProfile != "" && s.TrialRoutes == nil {
		var e error
		s.TrialRoutes, e = loadTrialRouteProfile(tasker, arg.TaskID, param.TrialProfile)
		if e != nil {
			log.Error().Err(e).Msg("Trial research route profile rejected")
			return false
		}
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return false
	}
	imageAt := time.Now()
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return false
	}
	capturedAt := time.Now()
	var tokens []screenocr.Token
	var v LiveView
	local := false
	if s.Started && param.RouteID == "" && s.Pending != nil && !s.Pending.IssuedAt.IsZero() {
		v, local, err = localInputView(s, *s.Pending, img, time.Now(), func(region maa.Rect) ([]screenocr.Token, error) {
			return screenocr.ReadRegion(ctx, img, region, false)
		})
		if err != nil {
			return false
		}
		// Emptying the tray returns to the complete cooking planner immediately.
		local = local && trayCount(v.Tray) > 0 && len(v.Unknown) == 0
	}
	if !local {
		tokens, err = screenocr.Read(ctx, img)
		if err != nil {
			return false
		}
		v = readTimedLive(ctx, img, tokens, s.Icons, s.Catalog)
		s.InputHUD = nil
		s.InputVisual = nil
		if v.Phase == "active" || v.Phase == "closing" || v.Phase == "closed_serving" {
			s.InputHUD = newCameraSearch(img, v.AddOrderBox, nil, false, imageAt)
			s.InputPhase = v.Phase
			visual := readLivePhase(img, nil, s.Icons, s.Catalog, v.Phase)
			s.InputVisual = &visual
		}
	} else {
		// Keep the original full-observation timestamp: successive local checks
		// cannot extend the two-second window indefinitely.
		log.Info().Str("component", "InnkeepingReactiveAction").Bool("local_observation", true).Dur("recognition_and_capture", time.Since(imageAt)).Msg("inn acknowledgement used fresh local controls")
	}
	v.TrayCapacity = s.trayCapacity()
	v = s.observeGourmand(v, tokens, func(box [4]int, seat string) bool {
		found, e := screenocr.ReadRegion(ctx, img, maa.Rect(box), true)
		return e == nil && confirmsSeatMarker(found, seat)
	})
	if param.RouteID != "" {
		if !s.prepareRouteProbe(ctx, param.RouteID, img, v) {
			s.announce(ctx, "Recorded inn route does not match the current pose, held dish or table request; no input sent.")
			_ = s.evidence(time.Now(), img, "route-probe-start-rejected")
			return false
		}
		return s.departFixedRoute(ctx, arg)
	}
	now := time.Now()
	if s.TravelPending {
		s.TravelPending = false
		s.SetupAt = now
	}
	if v.Phase == "ended" {
		if err := s.archiveEndedObservation(arg.TaskID, now, img, tokens); err != nil {
			log.Error().Err(err).Msg("inn ended-screen evidence could not be saved")
			return false
		}
		if s.Automatic && !s.Started {
			s.announce(ctx, "Clearing the previous inn result before preparing the requested session.")
			return route(ctx, arg, "InnkeepingSetupResultContinue")
		}
		s.announce(ctx, "Inn session finished. No new session will be started.")
		delete(a.sessions, arg.TaskID)
		if s.Automatic {
			return route(ctx, arg, "InnkeepingResultContinue")
		}
		return route(ctx, arg, "InnkeepingReactiveDone")
	}
	if !s.Started {
		if v.Phase != "active" && v.Phase != "kitchen" {
			if s.Automatic {
				if s.setupExpired(now) {
					s.announce(ctx, "Inn setup could not reach a recognized start screen within two minutes.")
					_ = s.evidence(now, img, "setup-blocked")
					return false
				}
				if v.Phase == "unknown" && setupTravelScreen(tokens) {
					return route(ctx, arg, "InnkeepingSetupTravel")
				}
				return route(ctx, arg, "InnkeepingStartDispatch")
			}
			s.announce(ctx, "Start an inn trial or session manually; reactive assistance is waiting.")
			return route(ctx, arg, "InnkeepingReactiveObserve")
		}
		// At the beginning no recipe can already have finished (shortest base time is 10s).
		// Joining later cannot prove the off-screen counter empty and would duplicate food.
		if v.RemainingSeconds == 0 {
			return route(ctx, arg, "InnkeepingReactiveObserve")
		}
		startedHere := s.Automatic && taskStartedInn(tasker, arg.TaskID)
		log.Info().Bool("native_start_verified", startedHere).Int("remaining_seconds", v.RemainingSeconds).Msg("inn initial session admission")
		if (v.RemainingSeconds < 290 && !startedHere) || len(v.Tray) > 0 {
			s.announce(ctx, "Start assistance before opening the inn. Mid-session counter stock cannot yet be synchronized safely.")
			return false
		}
		s.Started = true
		s.SeenAt = now
		if s.Automatic {
			s.announce(ctx, "Inn cooking and marker navigation active. Si handles drinks and bills.")
		} else {
			s.announce(ctx, "Reactive inn assistance active. Move between the counter and tables manually.")
		}
	}
	if v.Phase != "active" && v.Phase != "kitchen" && v.Phase != "closing" && v.Phase != "closed_serving" {
		s.announce(ctx, "Inn screen is blocked or unrecognized; no input sent.")
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	if len(v.Unknown) > 0 {
		if s.UncertainSince.IsZero() {
			s.UncertainSince = now
		}
		if now.Sub(s.UncertainSince) > 5*time.Second {
			s.announce(ctx, fmt.Sprintf("Unrecognized inn inventory (%v): stopped for inspection.", v.Unknown))
			_ = s.evidence(now, img, "unrecognized")
			return false
		}
		s.announce(ctx, "Waiting for animated food icons to settle; no input sent.")
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	s.UncertainSince = time.Time{}
	if v.Phase != "kitchen" {
		s.KitchenWaitUntil = time.Time{}
		s.Tray = v.Tray
	}
	if p := s.Pending; p != nil {
		if p.IssuedAt.IsZero() {
			return route(ctx, arg, "InnkeepingReactiveClick")
		}
		verified := false
		switch p.Kind {
		case "open":
			verified = v.Phase == "kitchen"
		case "select":
			verified = v.Kitchen == p.Kitchen
		case "close":
			verified = v.Phase != "kitchen"
		case "welcome":
			verified = v.Phase != "kitchen" && (v.Prompt == "serve" || len(v.GourmandRequests) > 0)
		case "queue":
			verified = queuedInputConfirmed(*p, v)
		case "select_tray":
			verified = v.Phase != "kitchen" && v.SelectedSlot == p.Slot && selectedTrayDish(v) == p.Dish
		case "pickup":
			actual := confirmedPickup(*p, v, s.Jobs)
			verified = actual != ""
			if verified && actual != p.Dish {
				p.RequestedDish = p.Dish
				p.Dish = actual
				p.Before = p.TrayBefore[actual]
				s.announce(ctx, fmt.Sprintf("Pickup target changed as food finished; verified %s in the tray.", actual))
			}
		case "serve":
			verified = v.Phase != "kitchen" && v.Tray[p.Dish] < p.Before && (!p.Gourmand || p.GourmandReconciled)
		}
		if !verified {
			if now.Sub(p.At) > 5*time.Second {
				s.announce(ctx, fmt.Sprintf("Inn %s action could not be verified; stopped without repeating it.", p.Kind))
				_ = s.evidence(now, img, "unverified-"+p.Kind)
				return false
			}
			return route(ctx, arg, "InnkeepingReactiveObserve")
		}
		if err := s.confirmRoute(p.Kind, p.Dish, v); err != nil {
			return false
		}
		switch p.Kind {
		case "queue":
			phase := "queued"
			if (p.Before == 0 && len(v.Queue) == 1) || (p.BeforeSeconds > 0 && v.ActiveDish == p.Dish && v.ActiveSeconds > p.BeforeSeconds+2) {
				phase = "cooking"
			}
			job := estimatedCook(s.Jobs, *p, s.Kitchens[p.Kitchen], v.QueueVerified && trayCount(v.Queue) == 1 && v.ActiveDish == p.Dish, now)
			job.ID, job.Phase = fmt.Sprintf("task%d-job%d", arg.TaskID, now.UnixNano()), phase
			s.Jobs = append(s.Jobs, job)
			startEvents, e := s.Tracker.Observe(p.At, []TimingObservation{{ID: job.ID, Kind: "cook", Phase: "queued", Kitchen: job.Kitchen, Chef: s.Kitchens[job.Kitchen].Chef, Dish: job.Dish}})
			if e != nil {
				return false
			}
			if AppendEvents(fsutil.OutputPath("data", "Innkeeping", "events.jsonl"), startEvents) != nil {
				return false
			}

		case "serve":
			s.confirmGourmandServing(*p)
			v = s.gourmandView(v)
			s.afterServed(now)
			s.ServeTried = [5]bool{}
		case "pickup":
			s.Navigation = navigationState{Goal: "counter", PickupZone: true, Since: now, LegStarted: now}
			s.ServeTried = [5]bool{}
			s.Jobs = retirePickedJob(s.Jobs, p.Dish, now)
		}
		if err := s.evidence(now, img, p.Kind); err != nil {
			log.Error().Err(err).Msg("inn evidence save failed")
			return false
		}
		log.Info().Str("kind", p.Kind).Str("dish", p.Dish).Str("kitchen", p.Kitchen).Msg("inn action visually verified")
		audit, _ := json.Marshal(struct {
			At            time.Time `json:"at"`
			Kind          string    `json:"kind"`
			Dish          string    `json:"dish,omitempty"`
			Kitchen       string    `json:"kitchen,omitempty"`
			RequestedDish string    `json:"requested_dish,omitempty"`
		}{now, p.Kind, p.Dish, p.Kitchen, p.RequestedDish})
		path := fsutil.OutputPath("data", "Innkeeping", "actions.jsonl")
		f, writeErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if writeErr != nil {
			return false
		}
		_, writeErr = f.Write(append(audit, '\n'))
		f.Close()
		if writeErr != nil {
			return false
		}
		s.Pending = nil
	}
	if v.Kitchen != "" {
		if s.Navigation.Goal != "" {
			s.Navigation.Since = now
		}
		k := ChefKitchen{ID: v.Kitchen, Capacity: 3, Verified: v.QueueVerified, CookSeconds: map[string]float64{}, Available: map[string]bool{}}
		for _, r := range v.Recipes {
			k.CookSeconds[r.Dish] = r.Seconds
			k.Available[r.Dish] = recipeQueueable(v, r)
		}
		if v.Kitchen == "1" {
			k.Chef = "Kitchen 1"
		} else {
			k.Chef = "Kitchen 2"
		}
		k.Specialties = kitchenSpecialties(tokens)
		chefs := []string{}
		for _, token := range tokens {
			if token.Score > .85 && token.Box[0] < 400 && token.Box[1] > 330 && token.Box[1] < 380 && strings.Contains(token.Text, "Chef") {
				chefs = append(chefs, token.Text)
			}
		}
		if len(chefs) > 0 {
			k.Chef = strings.Join(chefs, " + ")
		}

		// The displayed recipe time already incorporates chef bonuses; no inferred multiplier.
		s.Kitchens[v.Kitchen] = k
		// Adopt already queued work seen during initial kitchen synchronization.
		// Completed off-screen dishes remain reserved until a verified pickup.
		owned := map[string]int{}
		for _, j := range s.Jobs {
			if j.Kitchen == v.Kitchen {
				owned[j.Dish]++
			}
		}
		for dish, count := range v.Queue {
			for n := owned[dish]; n < count; n++ {
				s.Jobs = append(s.Jobs, cookJob{ID: fmt.Sprintf("task%d-observed%d", arg.TaskID, time.Now().UnixNano()), Dish: dish, Kitchen: v.Kitchen, Phase: "queued"})
			}
		}
		reconcileCookCountdown(s.Jobs, v, k, capturedAt)

	}
	if v.Prompt == "pickup" {
		if i := trackedCopyIndex(s.Jobs, v.PromptDish, now); i >= 0 {
			s.Jobs[i].Phase = "ready"
		}
	}
	if err := s.record(now, v); err != nil {
		log.Error().Err(err).Msg("inn telemetry write failed")
		return false
	}
	state := s.state(v)
	decision := PlanReactive(state)
	if decision.Kind == "queue" && v.RemainingSeconds == 0 {
		decision = ReactiveDecision{Kind: "wait", Reason: "Remaining service time is unknown."}
	}
	report := struct {
		At             time.Time                 `json:"at"`
		View           LiveView                  `json:"view"`
		Decision       ReactiveDecision          `json:"decision"`
		Movement       string                    `json:"movement"`
		State          ReactiveState             `json:"state"`
		Cooking        []cookJob                 `json:"cooking"`
		NextCompletion *cookJob                  `json:"next_completion,omitempty"`
		Gourmands      map[string]*gourmandVisit `json:"gourmands,omitempty"`
	}{now, v, decision, map[bool]string{true: "visual markers", false: "manual"}[s.Automatic], state, s.Jobs, nextCookCompletion(s.Jobs, v), s.Gourmands}
	data, err := json.MarshalIndent(report, "", "    ")
	if err != nil {
		return false
	}
	if err = fsutil.WriteFileAtomic(fsutil.OutputPath("data", "Innkeeping", "latest-state.json"), data, 0644); err != nil {
		log.Warn().Err(err).Msg("inn live snapshot write deferred; durable event and action journals retained")
	}
	if v.Phase == "kitchen" {
		if kitchenExitRequired(state, decision) {
			// The kitchen overlay hides carried inventory. Use the retained
			// session tray, and collect due food before another kitchen tour.
			s.Pending = &pendingInput{Kind: "close", At: now}
			return s.clickControl(ctx, arg, [4]int{1220, 98, 28, 30})
		}
		for _, id := range []string{"1", "2"} {
			if !s.Kitchens[id].Verified {
				x := 190
				if id == "2" {
					x = 470
				}
				s.Pending = &pendingInput{Kind: "select", Kitchen: id, At: now}
				return s.clickControl(ctx, arg, [4]int{x, 128, 80, 16})
			}
		}
		if decision.Kind == "queue" {
			if decision.Kitchen != v.Kitchen {
				x := 190
				if decision.Kitchen == "2" {
					x = 470
				}
				s.Pending = &pendingInput{Kind: "select", Kitchen: decision.Kitchen, At: now}
				return s.clickControl(ctx, arg, [4]int{x, 128, 80, 16})
			}
			for _, r := range v.Recipes {
				if r.Dish == decision.Dish && recipeQueueable(v, r) && r.Seconds > 0 {
					if v.Queue[r.Dish] > 0 && v.ActiveDish == r.Dish && v.ActiveSeconds <= 0 {
						s.announce(ctx, "Waiting for a readable cooking timer before extending this queue.")
						return route(ctx, arg, "InnkeepingReactiveObserve")
					}
					s.Pending = &pendingInput{Kind: "queue", Dish: r.Dish, Kitchen: v.Kitchen, Before: v.Queue[r.Dish], BeforeSeconds: v.ActiveSeconds, At: now}
					s.announce(ctx, fmt.Sprintf("Cooking %s in kitchen %s (displayed %.0fs).", r.Dish, v.Kitchen, r.Seconds))
					return s.clickControl(ctx, arg, r.Box)
				}
			}
		}
		if decision.Kind == "wait" && state.HoldKitchen {
			s.announce(ctx, "Watching orders and useful batch timers before the next delivery departure.")
			return route(ctx, arg, "InnkeepingReactiveObserve")
		}
		if decision.Kind == "wait" && !state.CollectionDue && s.waitForImminentCook(v, now) {
			s.announce(ctx, "The current dish is finishing; checking its timer before leaving to collect.")
			return route(ctx, arg, "InnkeepingReactiveObserve")
		}
		s.Pending = &pendingInput{Kind: "close", At: now}
		return s.clickControl(ctx, arg, [4]int{1220, 98, 28, 30})
	}
	if v.Prompt == "welcome" {
		s.Pending = &pendingInput{Kind: "welcome", At: now}
		return s.clickControl(ctx, arg, v.PromptBox)
	}
	heldCount := trayCount(s.Tray)
	selected := selectedTrayDish(v)
	_, gourmandCompatible := s.gourmandServing(v, selected)
	if heldCount > 0 && (selected == "" || v.Prompt == "serve" && !greenServeControl(img, v.PromptBox) && !gourmandCompatible) {
		if v.SelectedSlot >= 0 && v.SelectedSlot < len(s.ServeTried) {
			s.ServeTried[v.SelectedSlot] = true
		}
		slot := nextTraySlot(v, s.ServeTried)
		if selected != "" {
			slot = nextDinerTraySlot(v, WorldFoods(img, s.Icons, false), s.ServeTried)
		}
		if gourmandSlot := s.gourmandTraySlot(v); gourmandSlot >= 0 {
			slot = gourmandSlot
		}
		if slot >= 0 {
			s.Pending = &pendingInput{Kind: "select_tray", Dish: v.TraySlots[slot], Slot: slot, At: now}
			return s.clickControl(ctx, arg, [4]int{448 + 79*slot, 590, 70, 70})
		}
	}
	// Ordinary diners require the green compatibility signal. A freshly bound
	// gourmand can instead be checked against its complete recipe constraints.
	if selected != "" && v.Prompt == "serve" && (greenServeControl(img, v.PromptBox) || gourmandCompatible) {
		if len(v.GourmandRequests) > 0 && v.GourmandSeat == "" {
			s.announce(ctx, "Gourmand requests are visible but the table is ambiguous; verifying its physical number before serving.")
			return route(ctx, arg, "InnkeepingReactiveObserve")
		}
		p := &pendingInput{Kind: "serve", Dish: selected, Before: s.Tray[selected], At: now}
		if slot, ok := s.gourmandServing(v, selected); ok {
			p.Seat = v.GourmandSeat
			p.Gourmand = true
			p.GourmandSlot = slot
			p.GourmandVisit = s.Gourmands[p.Seat].Visit
		}
		s.Pending = p
		return s.clickControl(ctx, arg, v.PromptBox)
	}
	held := 0
	for _, n := range s.Tray {
		held += n
	}
	if v.Prompt == "pickup" {
		if neededPickup(v, v.PromptDish) {
			s.Pending = &pendingInput{Kind: "pickup", Dish: v.PromptDish, Before: s.Tray[v.PromptDish], At: now}
			return s.clickControl(ctx, arg, v.PromptBox)
		}
	}
	if s.Automatic && held == 0 && len(collectibleDishes(s.Jobs, v.Seats, now)) > 0 {
		return route(ctx, arg, "InnkeepingReactiveNavigate")
	}
	if !s.needsCounterReturn(v) && kitchenOpenRequired(state, decision, s.Kitchens["1"].Verified && s.Kitchens["2"].Verified) {
		if v.AddOrderBox[2] > 0 {
			s.Pending = &pendingInput{Kind: "open", At: now}
			return s.clickControl(ctx, arg, v.AddOrderBox)
		}
	}
	if s.Automatic {
		return route(ctx, arg, "InnkeepingReactiveNavigate")
	}
	s.announce(ctx, "Move to ready food or a matching diner; cooking, pickup and serving use confirmed controls.")
	return route(ctx, arg, "InnkeepingReactiveObserve")
}

// kitchenOpenRequired keeps ready food ahead of another kitchen visit. In
// manual movement mode a due pickup must wait at the counter, rather than
// repeatedly opening a menu that kitchenExitRequired immediately closes.
func kitchenOpenRequired(state ReactiveState, decision ReactiveDecision, kitchensVerified bool) bool {
	return trayCount(state.Tray) == 0 && !state.CollectionDue && (decision.Kind == "queue" || !kitchensVerified)
}

func kitchenExitRequired(state ReactiveState, decision ReactiveDecision) bool {
	if state.Phase != "kitchen" {
		return false
	}
	return trayCount(state.Tray) > 0 || (!state.HoldKitchen && (state.CollectionDue || decision.Kind == "pickup" || decision.Kind == "collect")) || decision.Kind == "serve" || decision.Kind == "close"
}

// A nearly finished first dish needs one fresh timer check, not a close/open
// menu cycle. The deadline is per kitchen visit and cannot renew indefinitely.
func (s *liveSession) waitForImminentCook(v LiveView, now time.Time) bool {
	if trayCount(v.Tray) > 0 {
		return false
	}
	seconds := v.ActiveSeconds
	if next := nextCookCompletion(s.Jobs, v); next != nil {
		remaining := next.ExpectedAt.Sub(now).Seconds()
		if remaining > 0 && (seconds <= 0 || remaining < seconds) {
			seconds = remaining
		}
	}
	if seconds <= 0 || seconds > 2 {
		return false
	}
	if s.KitchenWaitUntil.IsZero() {
		s.KitchenWaitUntil = now.Add(3 * time.Second)
	}
	return now.Before(s.KitchenWaitUntil)
}

// queuedInputConfirmed handles a completion that races with inserting the same
// recipe: count stays constant but the visible active timer restarts.
func queuedInputConfirmed(p pendingInput, v LiveView) bool {
	if v.Kitchen != p.Kitchen || !v.QueueVerified {
		return false
	}
	if v.Queue[p.Dish] > p.Before {
		return true
	}
	return p.Before > 0 && v.Queue[p.Dish] == p.Before && p.BeforeSeconds > 0 && v.ActiveDish == p.Dish && v.ActiveSeconds > p.BeforeSeconds+2
}

// collectibleDishes limits a counter search to tracked completed work that still
// covers current demand. An estimate permits inspection; a visible prompt proves pickup.
func collectibleDishes(jobs []cookJob, seats []SeatView, now time.Time) map[string]bool {
	wanted := map[string]bool{}
	for _, seat := range seats {
		for _, dish := range seat.Dishes {
			wanted[dish] = true
		}
	}
	ready := map[string]bool{}
	for _, job := range jobs {
		if wanted[job.Dish] && (job.CounterCheckDue || job.Phase == "ready" || (!job.ExpectedAt.IsZero() && !now.Before(job.ExpectedAt))) {
			ready[job.Dish] = true
		}
	}
	return ready
}

// retirePickedJob preserves a future same-dish cook when a ready copy is collected.
func retirePickedJob(jobs []cookJob, dish string, now time.Time) []cookJob {
	if best := trackedCopyIndex(jobs, dish, now); best >= 0 {
		return append(jobs[:best], jobs[best+1:]...)
	}
	return jobs
}

// trackedCopyIndex uses the same copy identity when observing and retiring ready
// food. Earlier queued work in a slower kitchen may finish after a later job.
func trackedCopyIndex(jobs []cookJob, dish string, now time.Time) int {
	best := -1
	rank := func(j cookJob) int {
		if j.Phase == "ready" {
			return 4
		}
		if j.CounterCheckDue {
			return 3
		}
		if !j.ExpectedAt.IsZero() && !now.Before(j.ExpectedAt) {
			return 2
		}
		if !j.ExpectedAt.IsZero() {
			return 1
		}
		return 0
	}
	for i, j := range jobs {
		if j.Dish != dish {
			continue
		}
		if best < 0 || rank(j) > rank(jobs[best]) || (rank(j) == rank(jobs[best]) && !j.ExpectedAt.IsZero() && j.ExpectedAt.Before(jobs[best].ExpectedAt)) {
			best = i
		}
	}
	return best
}
