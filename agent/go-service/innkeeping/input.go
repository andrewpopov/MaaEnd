package innkeeping

import (
	"encoding/json"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"image"
	"os"
	"reflect"
	"time"
)

var _ maa.CustomActionRunner = &ReactiveClickAction{}

// ReactiveClickAction is the atomic input boundary. Pipeline owns the loop;
// issuing and acknowledging one click in a single callback makes pause safe.
type ReactiveClickAction struct{ owner *ReactiveAction }

// localInputView validates only world controls whose phase was just observed.
// It rereads the tray and all order icons from this image, never cached tokens.
// Missing/stale HUD identity falls back to the normal full observation path.
func localInputView(s *liveSession, p pendingInput, img image.Image, now time.Time, read func(maa.Rect) ([]screenocr.Token, error)) (LiveView, bool, error) {
	if s == nil || !s.InputHUD.matches(img, now) || (s.InputPhase != "active" && s.InputPhase != "closing" && s.InputPhase != "closed_serving") {
		return LiveView{}, false, nil
	}
	if p.Gourmand || len(s.Gourmands) > 0 && (p.Kind == "serve" || p.Kind == "select_tray") || p.Kind != "pickup" && p.Kind != "serve" && p.Kind != "select_tray" {
		return LiveView{}, false, nil
	}
	var tokens []screenocr.Token
	if p.Kind != "select_tray" || !p.IssuedAt.IsZero() {
		var err error
		tokens, err = read(maa.Rect{720, 390, 480, 80})
		if err != nil {
			return LiveView{}, false, err
		}
	}
	if p.Kind == "serve" && !p.IssuedAt.IsZero() {
		// Route learning needs the served seat's actual Dining/waiting state,
		// not an inferred completion based only on the carried count.
		extra, err := read(maa.Rect{234, 20, 708, 105})
		if err != nil {
			return LiveView{}, false, err
		}
		tokens = append(tokens, extra...)
	}
	return readLivePhase(img, tokens, s.Icons, s.Catalog, s.InputPhase), true, nil
}

// localNavigationView avoids a second full-screen OCR immediately after the
// planner. Fresh icon identities and completion badges must match the planner's
// image-only observation. Text-dependent seat phases never authorize movement.
func localNavigationView(s *liveSession, img image.Image, now time.Time, read func(maa.Rect) ([]screenocr.Token, error)) (LiveView, bool, error) {
	if s == nil || s.InputVisual == nil || !s.InputHUD.matches(img, now) || (s.InputPhase != "active" && s.InputPhase != "closing" && s.InputPhase != "closed_serving") {
		return LiveView{}, false, nil
	}
	visual := readLivePhase(img, nil, s.Icons, s.Catalog, s.InputPhase)
	if len(visual.Unknown) > 0 || !reflect.DeepEqual(visual.Seats, s.InputVisual.Seats) || !reflect.DeepEqual(visual.TraySlots, s.InputVisual.TraySlots) || visual.SelectedSlot != s.InputVisual.SelectedSlot {
		return LiveView{}, false, nil
	}
	tokens, err := read(maa.Rect{720, 390, 480, 80})
	if err != nil {
		return LiveView{}, false, err
	}
	names := map[string]string{}
	for _, dish := range s.Catalog {
		names[foldName(dish.Name)] = dish.ID
	}
	readWorldPrompt(&visual, tokens, names)
	// The actual label's white-pixel geometry was verified above. Preserve its
	// box for camera search without extending the full-observation lifetime.
	b := s.InputHUD.Box
	visual.AddOrderBox = [4]int{b.Min.X, b.Min.Y, b.Dx(), b.Dy()}
	return visual, true, nil
}

// eligibleInput revalidates the visible control after any intervening pause.
// A changed screen cancels the intent so the planner can observe and choose again.
func eligibleInput(v LiveView, p pendingInput) ([4]int, bool) {
	switch p.Kind {
	case "open":
		return v.AddOrderBox, v.Phase == "active" && v.AddOrderBox[2] > 0
	case "select":
		return p.Box, v.Phase == "kitchen" && v.Kitchen != "" && v.Kitchen != p.Kitchen
	case "close":
		return p.Box, v.Phase == "kitchen"
	case "queue":
		if v.Phase != "kitchen" || v.Kitchen != p.Kitchen || !v.QueueVerified {
			return p.Box, false
		}
		for _, r := range v.Recipes {
			if r.Dish == p.Dish && recipeQueueable(v, r) && r.Seconds > 0 && float64(v.RemainingSeconds) > r.Seconds {
				needed := false
				for _, seat := range v.Seats {
					if includes(seat.Dishes, p.Dish) {
						needed = true
					}
				}
				return r.Box, needed
			}
		}
	case "select_tray":
		if p.Slot >= 0 && p.Slot < len(v.TraySlots) {
			return [4]int{448 + 79*p.Slot, 590, 70, 70}, v.Phase != "kitchen" && v.TraySlots[p.Slot] == p.Dish && v.SelectedSlot != p.Slot
		}
		return p.Box, false
	case "welcome":
		return v.PromptBox, v.Phase == "active" && v.Prompt == "welcome" && len(v.Unknown) == 0
	case "pickup":
		return v.PromptBox, len(v.Unknown) == 0 && neededPickup(v, p.Dish) && v.Prompt == "pickup" && v.PromptDish == p.Dish
	case "serve":
		return v.PromptBox, v.Prompt == "serve" && v.Tray[p.Dish] > 0 && selectedTrayDish(v) == p.Dish
	}
	return p.Box, false
}

// Run issues at most one freshly validated click and records its actual send time.
func (a *ReactiveClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || a.owner == nil {
		return false
	}
	a.owner.mu.Lock()
	defer a.owner.mu.Unlock()
	s := a.owner.sessions[arg.TaskID]
	if s == nil || s.Pending == nil {
		return false
	}
	p := s.Pending
	if !p.IssuedAt.IsZero() {
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return false
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return false
	}
	started := time.Now()
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return false
	}
	captured := time.Now()
	v, local, err := localInputView(s, *p, img, time.Now(), func(region maa.Rect) ([]screenocr.Token, error) { return screenocr.ReadRegion(ctx, img, region, false) })
	if err != nil {
		return false
	}
	var tokens []screenocr.Token
	if !local {
		tokens, err = screenocr.Read(ctx, img)
		if err != nil {
			return false
		}
		v = readTimedLive(ctx, img, tokens, s.Icons, s.Catalog)
	}
	v.TrayCapacity = s.trayCapacity()
	v = s.gourmandView(v)
	if p.Gourmand {
		// This action already required the full fresh image above. Reuse its
		// OCR tokens for board binding instead of recognizing that image twice.
		v = s.observeGourmand(v, tokens, func(box [4]int, seat string) bool {
			found, e := screenocr.ReadRegion(ctx, img, maa.Rect(box), true)
			return e == nil && confirmsSeatMarker(found, seat)
		})
		if v.GourmandSeat != p.Seat {
			s.Pending = nil
			return route(ctx, arg, "InnkeepingReactiveObserve")
		}
	}
	if p.Kind == "queue" || p.Kind == "select" {
		if s.kitchenInputShouldExit(v, captured, time.Now()) {
			s.Pending = nil
			return route(ctx, arg, "InnkeepingReactiveObserve")
		}
	}
	validated := time.Now()
	box, ok := eligibleInput(v, *p)
	if p.Kind == "serve" && !greenServeControl(img, box) {
		slot, compatible := s.gourmandServing(v, p.Dish)
		order := s.Gourmands[p.Seat]
		ok = ok && p.Gourmand && compatible && v.GourmandSeat == p.Seat && slot == p.GourmandSlot && order != nil && order.Visit == p.GourmandVisit
	}
	if p.Kind == "queue" {
		decision := PlanReactive(s.state(v))
		if decision.Kind != "queue" || decision.Dish != p.Dish || decision.Kitchen != p.Kitchen {
			ok = false
		}
		occupied := 0
		for _, n := range v.Queue {
			occupied += n
		}
		if occupied >= 3 {
			ok = false
		}
		if v.Queue[p.Dish] > 0 && v.ActiveDish == p.Dish && v.ActiveSeconds <= 0 {
			ok = false
		}
		p.Before = v.Queue[p.Dish]
		p.BeforeSeconds = v.ActiveSeconds
		for _, recipe := range v.Recipes {
			if recipe.Dish == p.Dish {
				p.CookSeconds = recipe.Seconds
			}
		}
	}
	if !ok {
		s.Pending = nil
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	if p.Kind == "pickup" {
		p.TrayBefore = map[string]int{}
		for dish, count := range v.Tray {
			p.TrayBefore[dish] = count
		}
	}
	if err := s.evidence(time.Now(), img, "input-"+p.Kind); err != nil {
		return false
	}
	if err := writeInputTrace(arg.TaskID, *p, v, box, "validated"); err != nil {
		return false
	}
	p.IssuedAt = time.Now()
	prepared := time.Now()
	s.captureRouteEnd(img, v, p.Kind)
	// Keep the button down across several emulator frames, with paired release
	// owned by the native action. Short accepted taps can be missed by the game.
	detail, actionErr := ctx.RunAction("InnkeepingReactivePress", maa.Rect{box[0] + box[2]/2, box[1] + box[3]/2, 1, 1}, "", nil)
	accepted := actionErr == nil && detail != nil && detail.Success
	log.Info().Str("component", "InnkeepingReactiveClickAction").Str("kind", p.Kind).Bool("local_validation", local).Dur("capture", captured.Sub(started)).Dur("recognition", validated.Sub(captured)).Dur("evidence_and_prepare", prepared.Sub(validated)).Dur("press", time.Since(prepared)).Msg("inn input latency")
	p.At = time.Now()
	stage := "controller_accepted"
	if !accepted {
		stage = "controller_failed"
	}
	if err := writeInputTrace(arg.TaskID, *p, v, box, stage); err != nil {
		return false
	}
	if !accepted {
		s.announce(ctx, "Inn controller rejected the input; stopped without repeating it.")
		return false
	}
	return route(ctx, arg, "InnkeepingReactiveObserve")
}

// A queue can finish between planning and the final input image. Reconcile that
// image before deciding whether a pending menu command is still worthwhile.
func (s *liveSession) kitchenInputShouldExit(v LiveView, capturedAt, now time.Time) bool {
	reconcileCookCountdown(s.Jobs, v, s.Kitchens[v.Kitchen], capturedAt)
	state := s.stateAt(v, now)
	return kitchenExitRequired(state, PlanReactive(state))
}

// writeInputTrace separates controller acceptance from game acknowledgement.
// The captured view ties failed input to the selected dish and exact control.
func writeInputTrace(taskID int64, p pendingInput, v LiveView, box [4]int, stage string) error {
	f, err := os.OpenFile(fsutil.OutputPath("data", "Innkeeping", "input-attempts.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(struct {
		At     time.Time `json:"at"`
		TaskID int64     `json:"task_id"`
		Kind   string    `json:"kind"`
		Dish   string    `json:"dish,omitempty"`
		Stage  string    `json:"stage"`
		Box    [4]int    `json:"box"`
		View   LiveView  `json:"view"`
	}{time.Now(), taskID, p.Kind, p.Dish, stage, box, v})
}
