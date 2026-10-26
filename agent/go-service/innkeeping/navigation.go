package innkeeping

import (
	"image"
	"math"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/charactercontroller"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// WorldFood binds a recognized food icon to its actual floating world marker.
// Counter markers are circles; diner requests are wider capsules with a pointer.
type WorldFood struct {
	Dish    string `json:"dish"`
	Counter bool   `json:"counter"`
	Box     [4]int `json:"box"`
}

// WorldFoods extracts the measured white marker silhouettes before classifying
// their contents. HUD cards, plain furniture and unknown icons cannot be goals.
func WorldFoods(img image.Image, lib IconLibrary, counter bool) []WorldFood {
	result := worldFoodsMask(img, lib, counter, false)
	if counter {
		// Preserve the low-resolution distant-marker mask. A second, neutral-rim
		// pass separates large pickup rings that touch bright wooden furniture.
		for _, candidate := range worldFoodsMask(img, lib, true, true) {
			duplicate := false
			for _, old := range result {
				if old.Dish == candidate.Dish && abs(old.Box[0]+old.Box[2]/2-candidate.Box[0]-candidate.Box[2]/2) < 16 && abs(old.Box[1]+old.Box[3]/2-candidate.Box[1]-candidate.Box[3]/2) < 16 {
					duplicate = true
					break
				}
			}
			if !duplicate {
				result = append(result, candidate)
			}
		}
	}
	return result
}

func worldFoodsMask(img image.Image, lib IconLibrary, counter, strictRims bool) []WorldFood {
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) {
		return nil
	}
	// Counter pickup 1 can appear left of the quest panel after session startup.
	// Keep the same marker geometry and icon checks across the visible counter.
	const left, top, right, bottom, step = 40, 120, 1280, 560, 2
	const width = (right - left) / step
	const height = (bottom - top) / step
	seen := make([]bool, width*height)
	white := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		r >>= 8
		g >>= 8
		b >>= 8
		if counter {
			// A broad yellow/brown threshold connects white food rings to
			// the bright wooden counter and floor. Keep neutral white rims
			// and the observed bright gold Choice rims as separate colors.
			if strictRims {
				return min(r, g, b) >= 170 && max(r, g, b)-min(r, g, b) <= 25
			}
			return r >= 190 && g >= 180 && b >= 60
		}
		return min(r, g, b) >= 190 && max(r, g, b)-min(r, g, b) <= 35
	}
	result := []WorldFood{}
	for sy := 0; sy < height; sy++ {
		for sx := 0; sx < width; sx++ {
			start := sy*width + sx
			if seen[start] || !white(left+sx*step, top+sy*step) {
				continue
			}
			seen[start] = true
			queue := []int{start}
			minx, maxx, miny, maxy := sx, sx, sy, sy
			for head := 0; head < len(queue); head++ {
				i := queue[head]
				x, y := i%width, i/width
				minx = min(minx, x)
				maxx = max(maxx, x)
				miny = min(miny, y)
				maxy = max(maxy, y)
				for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					nx, ny := x+d[0], y+d[1]
					if nx < 0 || ny < 0 || nx >= width || ny >= height {
						continue
					}
					n := ny*width + nx
					if !seen[n] && white(left+nx*step, top+ny*step) {
						seen[n] = true
						queue = append(queue, n)
					}
				}
			}
			x, y, w, h := left+minx*step, top+miny*step, (maxx-minx+1)*step, (maxy-miny+1)*step
			minimumArea, minimumWidth, minimumHeight, maximumHeight := 200, 44, 30, 120
			if counter {
				// Far tables see ready-food rings around 26 pixels across. Keep
				// their dish and annulus checks instead of dropping their silhouette.
				minimumArea, minimumWidth, minimumHeight, maximumHeight = 35, 26, 26, 220
				if strictRims {
					minimumHeight = 60
				}
			}
			maximumWidth := 230
			if counter && strictRims {
				// A pickup rim can join a neighboring plate or diner bubble.
				// Retain that measured component for the existing exposed-ring fit.
				maximumWidth = 360
			}
			if len(queue) < minimumArea || w < minimumWidth || w > maximumWidth || h < minimumHeight || h > maximumHeight || (counter && y == top) || y+h >= bottom {
				continue
			}
			// The named interaction icon is UI, not a floating counter destination.
			if counter && x >= 700 && x < 810 && y >= 390 {
				continue
			}
			circle := float64(w)/float64(h) < 1.35
			if counter {
				if circle {
					id, _ := lib.Identify(img, x+w/2, y+int(float64(h)*.40), int(float64(w)*.35))
					if id == "" {
						// At dining-room distance a one-pixel rounding change is a
						// significant part of the dish. Inspect the measured larger
						// crop as well, keeping the same identity confidence margin.
						id, _ = lib.Identify(img, x+w/2, y+int(float64(h)*.46), int(float64(w)*.40))
					}
					// A ring without a connected pointer is centered halfway down
					// its component. Its icon still sits higher inside the rim.
					isolated := isolatedCounterBoundary(img, x+w/2, y+int(float64(h)*.40), int(float64(w)*.43)) ||
						isolatedCounterBoundary(img, x+w/2, y+h/2, int(float64(min(w, h))*.47))
					if strictRims {
						isolated = isolated || shadedCounterBoundary(img, [4]int{x, y, w, h})
					}
					if id != "" && isolated {
						result = append(result, WorldFood{id, true, [4]int{x, y, w, h}})
						continue
					}
				}
				// Touching ready-food rings merge into a wide white component.
				// Fit only exposed circular boundaries inside that observed component.
				result = append(result, splitCounterRings(img, lib, [4]int{x, y, w, h})...)
			} else if !circle {
				before := len(result)
				for _, fraction := range []float64{.28, .73} {
					cx, cy := x+int(float64(w)*fraction), y+int(float64(h)*.46)
					iconRadius := int(float64(h) * .34)
					// Served foods stay visible in world bubbles. Their green badge
					// excludes them from demand just as it does on the top order cards.
					if greenBadge(img, cx+iconRadius, cy+iconRadius) {
						continue
					}
					id, _ := lib.Identify(img, cx, cy, iconRadius)
					if id != "" {
						result = append(result, WorldFood{id, false, [4]int{x, y, w, h}})
					}
				}
				if len(result) == before && float64(w)/float64(h) < 2.1 {
					cx, cy, radius := x+w/2, y+int(float64(h)*.46), int(float64(h)*.34)
					if !greenBadge(img, cx+radius, cy+radius) {
						if id, _ := lib.Identify(img, cx, cy, radius); id != "" {
							result = append(result, WorldFood{id, false, [4]int{x, y, w, h}})
						}
					}
				}
			}
		}
	}
	return result
}

type navigationState struct {
	Goal          string
	Since         time.Time
	LegStarted    time.Time
	Scanned       int
	Last          [4]int
	Stationary    int
	Departed      bool
	PickupZone    bool
	BarAvoided    bool
	BarAdvance    bool
	BarBottom     int
	BarBest       int
	BarStationary int
	BenchAvoided  bool
}

type movementCommand struct {
	Kind               string
	X, Y, Milliseconds int
	Origin             [2]int `json:"Origin,omitempty"`
	Pipeline           bool   `json:"Pipeline,omitempty"`
	HoldMilliseconds   int    `json:"HoldMilliseconds,omitempty"`
	SharedJoystick     bool   `json:"SharedJoystick,omitempty"`
	JoystickWarmup     bool   `json:"JoystickWarmup,omitempty"`
	Goal               string
	Dish               string `json:"Dish,omitempty"`
	Box                [4]int
}

// issueMovement preserves the exact driver used to calibrate a route. Native
// controller swipes remain available for old recordings; pipeline gestures also
// retain endpoint holds instead of approximating a joystick hold with a drag.
func issueMovement(ctx *maa.Context, ctrl *maa.Controller, cmd movementCommand) bool {
	if cmd.SharedJoystick && pienv.ControllerType() == "Adb" {
		tasker := ctx.GetTasker()
		if tasker == nil {
			return false
		}
		// A native failure must stop this input; never replay it through another driver.
		return charactercontroller.HoldJoystick(ctrl, cmd.X, cmd.Y, time.Duration(cmd.HoldMilliseconds)*time.Millisecond, tasker.Stopping)
	}
	if cmd.Pipeline || cmd.HoldMilliseconds > 0 {
		result, err := ctx.RunActionDirect(maa.ActionTypeSwipe, movementSwipeParam(cmd), maa.Rect{}, nil)
		return err == nil && result != nil && result.Success
	}
	x, y := 195, 551
	if cmd.Kind == "camera" {
		x, y = 1000, 350
		if cmd.Origin != [2]int{} {
			x, y = cmd.Origin[0], cmd.Origin[1]
		}
	}
	return ctrl.PostSwipe(int32(x), int32(y), int32(cmd.X), int32(cmd.Y), time.Duration(cmd.Milliseconds)*time.Millisecond).Wait().Success()
}

func movementSwipeParam(cmd movementCommand) *maa.SwipeParam {
	x, y := 195, 551
	if cmd.Kind == "camera" {
		x, y = 1000, 350
		if cmd.Origin != [2]int{} {
			x, y = cmd.Origin[0], cmd.Origin[1]
		}
	}
	// Zero-sized rectangles extend to the screen edge in MaaFramework.
	// A one-pixel rectangle keeps randomized action targets at the recorded point.
	param := &maa.SwipeParam{
		Begin:    maa.NewTargetRect(maa.Rect{x, y, 1, 1}),
		End:      []maa.Target{maa.NewTargetRect(maa.Rect{cmd.X, cmd.Y, 1, 1})},
		Duration: []time.Duration{time.Duration(cmd.Milliseconds) * time.Millisecond},
		EndHold:  []time.Duration{time.Duration(cmd.HoldMilliseconds) * time.Millisecond},
	}
	if cmd.JoystickWarmup && cmd.Kind == "walk" {
		param.End = append([]maa.Target{maa.NewTargetRect(maa.Rect{x, y, 1, 1})}, param.End...)
		param.Duration = append([]time.Duration{time.Millisecond}, param.Duration...)
		param.EndHold = append([]time.Duration{50 * time.Millisecond}, param.EndHold...)
	}
	return param
}

func movementDurationMS(cmd movementCommand) int {
	total := cmd.Milliseconds + cmd.HoldMilliseconds
	if cmd.JoystickWarmup {
		total += 51
	}
	return total
}

func validJoystickWarmup(cmd movementCommand) bool {
	return !cmd.JoystickWarmup || cmd.Kind == "walk" && cmd.Milliseconds >= 80
}

// navigationGesture pushes the joystick promptly, then holds its direction.
// A slow drag spends most of the requested movement ramping through the dead zone.
// Existing saved routes retain their original gesture timing through issueMovement.
func navigationGesture(cmd movementCommand) movementCommand {
	if cmd.Kind == "camera" {
		// Use the same pipeline swipe path that visibly rotates the live game.
		// A controller-accepted direct swipe did not prove camera movement.
		if cmd.Origin == [2]int{} {
			// The old y=350 origin can hit the inn interaction menu. Translate
			// both endpoints to the measured camera area without changing the turn.
			cmd.Origin = [2]int{1000, 230}
			cmd.Y -= 120
		}
		cmd.Pipeline = true
		return cmd
	}
	if cmd.Kind != "camera" && !cmd.Pipeline && cmd.HoldMilliseconds == 0 {
		cmd.HoldMilliseconds = cmd.Milliseconds
		cmd.SharedJoystick = cmd.HoldMilliseconds > 0 && cmd.HoldMilliseconds <= 1500
		cmd.Milliseconds = 20
		cmd.Pipeline = true
	}
	return cmd
}

// planMovement steers toward a freshly visible compatible marker. A full camera
// search or a stationary target ends the leg instead of repeating a blind route.
func planMovement(n *navigationState, now time.Time, goals []WorldFood, wanted map[string]bool, counter bool) (movementCommand, string) {
	goal := "counter"
	if !counter {
		goal = "diner"
	}
	if n.Goal != goal {
		*n = navigationState{Goal: goal, Since: now}
	}
	if n.LegStarted.IsZero() {
		n.LegStarted = now
	}
	if now.Sub(n.LegStarted) > 2*time.Minute {
		return movementCommand{}, "The navigation leg exceeded two minutes; stopped without restarting its progress budget."
	}
	best := -1
	for i, g := range goals {
		if wanted[g.Dish] && (best < 0 || g.Box[1]+g.Box[3] > goals[best].Box[1]+goals[best].Box[3]) {
			best = i
		}
	}
	if best < 0 {
		if n.Scanned >= 1440 {
			return movementCommand{}, "A full camera search found no visible matching food or diner marker."
		}
		n.Scanned += 90
		n.Last = [4]int{}
		n.Stationary = 0
		return movementCommand{Kind: "camera", X: 910, Y: 350, Milliseconds: 250, Goal: goal}, ""
	}
	// Finding a target completes the separately bounded camera search.
	if n.Scanned > 0 {
		n.Since = now
	}
	if now.Sub(n.Since) > 40*time.Second {
		return movementCommand{}, "No navigation progress was verified for 40 seconds."
	}
	b := goals[best].Box
	if n.Last[2] > 0 && abs(b[0]-n.Last[0])+abs(b[1]-n.Last[1])+abs(b[2]-n.Last[2])+abs(b[3]-n.Last[3]) < 7 {
		n.Stationary++
	} else {
		n.Stationary = 0
		n.Since = now
		n.Last = b
	}
	if n.Stationary >= 3 {
		return movementCommand{}, "The target is stationary after movement; a bench or counter blocks this approach."
	}
	n.Scanned = 0
	dx := float64(b[0] + b[2]/2 - 640)
	x, y := 195, 471
	if math.Abs(dx) > 90 {
		x = 195 + int(math.Copysign(55, dx))
		y = 491
	}
	if math.Abs(dx) > 270 {
		x = 195 + int(math.Copysign(80, dx))
		y = 511
	}
	duration := 450
	if b[1]+b[3] > 340 {
		duration = 250
	}
	return movementCommand{Kind: "walk", X: x, Y: y, Milliseconds: duration, Goal: goal, Dish: goals[best].Dish, Box: b}, ""
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

var _ maa.CustomActionRunner = &ReactiveNavigateAction{}

// ReactiveNavigateAction performs one visual servo step; Pipeline owns repetition.
type ReactiveNavigateAction struct{ owner *ReactiveAction }

// Run captures and revalidates the world before sending one short movement.
func (a *ReactiveNavigateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || a.owner == nil {
		return false
	}
	a.owner.mu.Lock()
	defer a.owner.mu.Unlock()
	s := a.owner.sessions[arg.TaskID]
	if s == nil || !s.Automatic || !s.Started {
		return false
	}
	t := ctx.GetTasker()
	if t == nil {
		return false
	}
	ctrl := t.GetController()
	if ctrl == nil {
		return false
	}
	started := time.Now()
	ctrl.PostScreencap().Wait()
	img, e := ctrl.CacheImage()
	if e != nil || img == nil {
		return false
	}
	captured := time.Now()
	var ocrDuration time.Duration
	v, local, e := localNavigationView(s, img, time.Now(), func(roi maa.Rect) ([]screenocr.Token, error) {
		before := time.Now()
		tokens, err := screenocr.ReadRegion(ctx, img, roi, false)
		ocrDuration += time.Since(before)
		return tokens, err
	})
	if e != nil {
		return false
	}
	if !local {
		before := time.Now()
		tokens, err := screenocr.Read(ctx, img)
		ocrDuration += time.Since(before)
		if err != nil {
			return false
		}
		v = ReadLive(img, tokens, s.Icons, s.Catalog)
	}
	v = s.gourmandView(v)
	log.Info().Str("component", "InnkeepingReactiveNavigateAction").Bool("local_validation", local).
		Dur("capture", captured.Sub(started)).Dur("ocr", ocrDuration).
		Dur("image_validation", time.Since(captured)-ocrDuration).
		Dur("capture_and_recognition", time.Since(started)).Msg("inn navigation perception latency")
	if (v.Phase != "active" && v.Phase != "closing" && v.Phase != "closed_serving") || len(v.Unknown) > 0 || actionableInteraction(img, v) {
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	held := 0
	wanted := map[string]bool{}
	for dish, count := range v.Tray {
		held += count
		if count > 0 {
			wanted[dish] = true
		}
	}
	counter := held == 0 || s.collectingBatch(v, time.Now())
	legGoal := "diner"
	if counter {
		legGoal = "counter"
	}
	if s.Navigation.Goal == legGoal && !s.Navigation.LegStarted.IsZero() && time.Since(s.Navigation.LegStarted) > 2*time.Minute {
		s.announce(ctx, "The navigation leg exceeded two minutes; stopped before further movement.")
		_ = s.evidence(time.Now(), img, "navigation-deadline")
		return false
	}
	if counter {
		wanted = readyForTray(s.Jobs, v, time.Now())
	}
	if len(wanted) == 0 && !s.needsCounterReturn(v) {
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	if s.TrialRoutes != nil {
		if s.startTrialRoute(img, v) {
			return s.departFixedRoute(ctx, arg)
		}
		s.announce(ctx, "Trial route has no measured connection from this pose and inventory; stopped for inspection.")
		_ = s.evidence(time.Now(), img, "trial-route-start-rejected")
		return false
	}
	if s.startTransitRoute(ctx, img, v, counter) {
		s.announce(ctx, "Following the verified middle-lane route; checking each physical endpoint.")
		return s.departFixedRoute(ctx, arg)
	}
	// A visible food bubble does not establish a walkable route. Keep legacy
	// steering and detour helpers for offline analysis, never as a live fallback.
	s.announce(ctx, "No verified middle-lane route from this position; movement stopped for calibration.")
	_ = s.evidence(time.Now(), img, "middle-lane-route-missing")
	return false
}

var _ maa.CustomActionRunner = &InnSetupNavigateAction{}

// InnSetupNavigateAction requests the shared teleport route once. Pipeline owns
// the landmark-checked exterior, doorway and ledger approach after arrival.
type InnSetupNavigateAction struct{ owner *ReactiveAction }

// Run never navigates toward Si: her interaction opens dialogue, not the inn.
func (a *InnSetupNavigateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || a.owner == nil {
		return false
	}
	a.owner.mu.Lock()
	defer a.owner.mu.Unlock()
	s := a.owner.sessions[arg.TaskID]
	if s == nil || !s.Automatic || s.Started {
		return false
	}
	t := ctx.GetTasker()
	if t == nil {
		return false
	}
	ctrl := t.GetController()
	if ctrl == nil {
		return false
	}
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return false
	}
	tokens, err := screenocr.Read(ctx, img)
	if err != nil {
		return false
	}
	if _, ok := innOpenControl(tokens); ok {
		return route(ctx, arg, "InnkeepingStartDispatch")
	}
	mapVerified := false
	if !setupTravelScreen(tokens) && setupTravelPermitted(tokens, true) {
		// Reuse the scene manager's map recognition for ship maps without the
		// ordinary Explore heading or regional map footer labels.
		detail, recognitionErr := ctx.RunRecognition("InMapAny", img)
		mapVerified = recognitionErr == nil && detail != nil && detail.Hit
	}
	if s.TravelAttempted || !setupTravelPermitted(tokens, mapVerified) {
		s.announce(ctx, "Inn entry does not match a recorded checkpoint; stopped without searching for an NPC or repeating travel.")
		_ = s.evidence(time.Now(), img, "entrance-blocked")
		return false
	}
	s.TravelAttempted = true
	s.TravelPending = true
	s.announce(ctx, "Travelling to Yuanyuan Manor, then following the recorded courtyard and inside aisle to the inn ledger.")
	return route(ctx, arg, "InnkeepingTravelToManor")
}

// setupTravelScreen requires positive world or supported menu evidence. Missing
// inn OCR alone never authorizes leaving an active or unrecognized screen.
func setupTravelScreen(tokens []screenocr.Token) bool {
	phase := Observe(tokens).Phase
	if phase != "world" && phase != "unknown" {
		return false
	}
	markerTags, regionOverview := false, false
	for _, token := range tokens {
		if token.Score < .90 || token.Box[1] < 580 {
			continue
		}
		markerTags = markerTags || token.Box[0] < 300 && foldName(token.Text) == "markertags"
		regionOverview = regionOverview || token.Box[0] > 900 && foldName(token.Text) == "regionoverview"
	}
	if markerTags && regionOverview {
		return true
	}
	for _, token := range tokens {
		if token.Score < .90 || token.Box[1] > 100 {
			continue
		}
		switch foldName(token.Text) {
		case "explore", "equip", "gear", "weapon", "operators", "backpack", "valuablesstash", "arsenal", "depot":
			return true
		}
	}
	return false
}

// counterDeparture leaves the confirmed pickup zone before scanning diners.
// Translation separates overlapping table markers that camera rotation cannot.
func counterDeparture(n *navigationState, now time.Time, anchors []WorldFood) (movementCommand, bool) {
	if n.Goal == "diner" && (n.Departed || n.Scanned > 0) {
		return movementCommand{}, false
	}
	best := -1
	for i, a := range anchors {
		if a.Counter && (best < 0 || abs(a.Box[0]+a.Box[2]/2-640) < abs(anchors[best].Box[0]+anchors[best].Box[2]/2-640)) {
			best = i
		}
	}
	// The last dish removes its floating marker. A just-verified pickup and
	// fresh Put Back prompt still prove this is the pickup zone, before any scan.
	if best < 0 && (!n.PickupZone || n.Scanned != 0) {
		return movementCommand{}, false
	}
	b := [4]int{640, 0, 0, 0}
	if best >= 0 {
		b = anchors[best].Box
	}
	dx := max(-55, min(55, (640-b[0]-b[2]/2)/4))
	*n = navigationState{Goal: "diner", Since: now, LegStarted: now, Departed: true}
	return movementCommand{Kind: "leave_counter", X: 195 + dx, Y: 621, Milliseconds: 500, Goal: "diner", Box: b}, true
}

// actionableInteraction yields to a valid nearby action, never a disabled Serve
// or an unwanted counter dish that would trap the navigation loop.
func actionableInteraction(img image.Image, v LiveView) bool {
	if v.Prompt == "welcome" {
		return true
	}
	if v.Prompt == "serve" {
		return greenServeControl(img, v.PromptBox) && len(v.Tray) > 0
	}
	if v.Prompt == "pickup" && neededPickup(v, v.PromptDish) {
		return true
	}
	return false
}

// floorClearance checks the measured lateral landing area for low-chroma stone
// floor, rejecting the drinks cabinet and benches before selecting a detour.
func floorClearance(img image.Image, right bool) float64 {
	if img == nil {
		return 0
	}
	left := 420
	if right {
		left = 760
	}
	clear, total := 0, 0
	for y := 450; y < 560; y += 3 {
		for x := left; x < left+100; x += 3 {
			r, g, b, _ := img.At(x, y).RGBA()
			r >>= 8
			g >>= 8
			b >>= 8
			if min(r, g, b) >= 70 && max(r, g, b)-min(r, g, b) < 55 {
				clear++
			}
			total++
		}
	}
	return float64(clear) / float64(total)
}

// barDetour follows the observed free floor around the protruding drinks bar;
// every subsequent forward step still requires a freshly recognized food goal.
func barDetour(n *navigationState, now time.Time, img image.Image, v LiveView, goals []WorldFood, wanted map[string]bool) (movementCommand, bool, string) {
	if v.Prompt != "bar" || n.Goal == "counter" && n.BarAvoided {
		return movementCommand{}, false, ""
	}
	best := -1
	for i, g := range goals {
		if g.Counter && wanted[g.Dish] && (best < 0 || g.Box[1]+g.Box[3] > goals[best].Box[1]+goals[best].Box[3]) {
			best = i
		}
	}
	if best < 0 {
		return movementCommand{}, false, ""
	}
	legStarted := now
	if n.Goal == "counter" && !n.LegStarted.IsZero() {
		legStarted = n.LegStarted
	}
	if now.Sub(legStarted) > 2*time.Minute {
		return movementCommand{}, false, "The navigation leg exceeded two minutes; no detour was sent."
	}
	b := goals[best].Box
	right := b[0]+b[2]/2 < 640
	if floorClearance(img, right) < .5 {
		return movementCommand{}, false, "No clear floor was verified beside the drinks bar; stopped before moving into furniture."
	}
	*n = navigationState{Goal: "counter", Since: now, LegStarted: legStarted, BarAvoided: true, BarAdvance: true, BarBottom: b[1] + b[3], BarBest: b[1] + b[3]}
	x := 115
	if right {
		x = 275
	}
	return movementCommand{Kind: "leave_bar", X: x, Y: 551, Milliseconds: 650, Goal: "counter", Box: b}, true, ""
}

// atPickupCounter also handles another ready dish taking over the interaction
// label after pickup; that does not mean the player has left the counter.
func atPickupCounter(v LiveView) bool {
	return len(v.Tray) > 0 && (v.Prompt == "put_back" || v.Prompt == "pickup")
}

// setupTravelPermitted accepts shared native map evidence only outside an inn session.
func setupTravelPermitted(tokens []screenocr.Token, mapVerified bool) bool {
	phase := Observe(tokens).Phase
	if phase != "world" && phase != "unknown" {
		return false
	}
	return setupTravelScreen(tokens) || mapVerified
}
