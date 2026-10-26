package innkeeping

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Scene fingerprints use the upper architecture visible in captured counter
// and dining-room views, outside the quest overlay, food markers and player.
// Both textured regions must match: a route also fixes camera yaw.
type scenePatch struct {
	Box              [4]int  `json:"box"`
	Pixels           []uint8 `json:"pixels"`
	ArchitectureOnly bool    `json:"architecture_only,omitempty"`
}
type sceneFingerprint [2]scenePatch

var routeSceneBoxes = [2]image.Rectangle{image.Rect(570, 150, 690, 190), image.Rect(850, 170, 1010, 210)}

// UnmarshalJSON preserves earlier calibrated route files while new captures
// retain the individual architectural crop chosen at each endpoint.
func (s *sceneFingerprint) UnmarshalJSON(data []byte) error {
	var old [2][]uint8
	if err := json.Unmarshal(data, &old); err == nil {
		for i, pixels := range old {
			b := routeSceneBoxes[i]
			s[i] = scenePatch{Box: [4]int{b.Min.X, b.Min.Y, b.Dx(), b.Dy()}, Pixels: pixels}
		}
		return nil
	}
	type plain sceneFingerprint
	return json.Unmarshal(data, (*plain)(s))
}

func sampleScene(img image.Image, box [4]int) scenePatch {
	p := scenePatch{Box: box}
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) || box[0] < 0 || box[0]+box[2] > 1280 || box[1] < 0 || box[1]+box[3] > 720 || box[2] < 80 || box[2] > 160 || box[3] != 40 {
		return p
	}
	for y := box[1]; y < box[1]+box[3]; y += 8 {
		for x := box[0]; x < box[0]+box[2]; x += 8 {
			r, g, b, _ := img.At(x, y).RGBA()
			p.Pixels = append(p.Pixels, uint8(r>>8), uint8(g>>8), uint8(b>>8))
		}
	}
	return p
}

func patchTexture(p scenePatch) int {
	texture := 0
	for i := 3; i < len(p.Pixels); i++ {
		if p.ArchitectureOnly && (!architecturePixel(p.Pixels, (i/3)*3) || !architecturePixel(p.Pixels, ((i-3)/3)*3)) {
			continue
		}
		texture += abs(int(p.Pixels[i]) - int(p.Pixels[i-3]))
	}
	return texture
}

func architecturePixel(p []uint8, i int) bool {
	if i+2 >= len(p) {
		return false
	}
	r, g, b := int(p[i]), int(p[i+1]), int(p[i+2])
	return r >= 10 && r <= 235 && r >= g-3 && g >= b-3 && r-g <= 75 && g-b <= 65
}

func architectureCoverage(p scenePatch) int {
	n := 0
	for i := 0; i < len(p.Pixels); i += 3 {
		if architecturePixel(p.Pixels, i) {
			n++
		}
	}
	return n
}

func fingerprint(img image.Image) sceneFingerprint {
	var out sceneFingerprint
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) {
		return out
	}
	// Preserve the original landmark choices for existing routes. Narrow
	// jambs beside the bright window are a fallback only when the original
	// crops cannot provide a textured architectural reference.
	for i := range out {
		for pass := 0; pass < 2; pass++ {
			xs := [2][]int{{570, 630, 690}, {850, 910, 950}}[i]
			ys, widths := []int{150, 170}, []int{120}
			if pass == 1 {
				xs = [2][]int{{500, 510, 570, 630, 690}, {850, 910, 950}}[i]
				ys, widths = []int{150, 170, 190}, []int{80, 120}
			}
			for _, x := range xs {
				for _, y := range ys {
					for _, width := range widths {
						p := sampleScene(img, [4]int{x, y, width, 40})
						p.ArchitectureOnly = true
						if architectureCoverage(p)*6 < len(p.Pixels) {
							continue
						}
						if patchTexture(p) > patchTexture(out[i]) {
							out[i] = p
						}
					}
				}
			}
			if len(out[i].Pixels) > 0 && patchTexture(out[i]) >= architectureCoverage(out[i])*6 {
				break
			}
		}
	}
	return out
}

func sceneMatchesImage(img image.Image, reference sceneFingerprint) bool {
	// Each architectural crop gets a tiny settling tolerance; slight depth
	// changes produce different parallax at the two independent landmarks.
	for _, p := range reference {
		found := false
		for dy := -4; dy <= 4 && !found; dy += 2 {
			for dx := -6; dx <= 6 && !found; dx += 2 {
				box := p.Box
				box[0] += dx
				box[1] += dy
				current := sampleScene(img, box)
				current.Box = p.Box
				current.ArchitectureOnly = p.ArchitectureOnly
				found = scenePatchSetMatches([]scenePatch{current}, []scenePatch{p})
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func sceneMatches(a, b sceneFingerprint) bool {
	return scenePatchSetMatches(a[:], b[:])
}

func scenePatchSetMatches(a, b []scenePatch) bool {
	for i := range a {
		box := a[i].Box
		if box[0] < 500 || box[0]+box[2] > 1110 || box[1] < 140 || box[1]+box[3] > 540 {
			return false
		}
		expected := ((box[2] + 7) / 8) * ((box[3] + 7) / 8) * 3
		if box != b[i].Box || expected == 0 || len(a[i].Pixels) != expected || len(b[i].Pixels) != expected {
			return false
		}
		// A flat wall or sky cannot locate a starting position reliably.
		minimumTexture := expected * 4
		if b[i].ArchitectureOnly {
			minimumTexture = architectureCoverage(b[i]) * 6
		}
		if patchTexture(b[i]) < minimumTexture {
			return false
		}
		var lighting [3]float64
		var channelCount [3]int
		for j, p := range a[i].Pixels {
			if b[i].ArchitectureOnly && !architecturePixel(b[i].Pixels, (j/3)*3) {
				continue
			}
			lighting[j%3] += float64(int(p) - int(b[i].Pixels[j]))
			channelCount[j%3]++
		}
		for c := range lighting {
			if channelCount[c] == 0 {
				return false
			}
			lighting[c] /= float64(channelCount[c])
			if lighting[c] < -25 || lighting[c] > 25 {
				return false
			}
		}
		sum, changed, compared := 0.0, 0, 0
		for j, p := range a[i].Pixels {
			if b[i].ArchitectureOnly && !architecturePixel(b[i].Pixels, (j/3)*3) {
				continue
			}
			compared++
			d := float64(int(p)-int(b[i].Pixels[j])) - lighting[j%3]
			if d < 0 {
				d = -d
			}
			sum += d
			if d > 24 {
				changed++
			}
		}
		if compared < expected/3 || sum > float64(compared*12) || changed > compared/5 {
			return false
		}
	}
	return true
}

type fixedRoute struct {
	Version         int               `json:"version"`
	ID              string            `json:"id"`
	Goal            string            `json:"goal"`
	Seat            string            `json:"seat"`
	Start           sceneFingerprint  `json:"start"`
	End             sceneFingerprint  `json:"end"`
	Steps           []movementCommand `json:"steps"`
	CreatedAt       time.Time         `json:"created_at"`
	Replays         int               `json:"successful_replays"`
	CalibrationOnly bool              `json:"calibration_only,omitempty"`
	Alignment       *routeAlignment   `json:"alignment,omitempty"`
	SeatMarker      [4]int            `json:"seat_marker,omitempty"`
	CounterHub      bool              `json:"counter_hub,omitempty"`
	StartSeatMarker [4]int            `json:"start_seat_marker,omitempty"`
	SeatTemplate    string            `json:"seat_template,omitempty"`
	FromWaypoint    string            `json:"from_waypoint,omitempty"`
	ToWaypoint      string            `json:"to_waypoint,omitempty"`
	MovementStage   string            `json:"movement_stage,omitempty"`
	Lane            string            `json:"lane,omitempty"`
	InnReplays      int               `json:"inn_replays,omitempty"`
	LoadedReplays   int               `json:"loaded_replays,omitempty"`
	EmptyReplays    int               `json:"empty_replays,omitempty"`
	Evidence        *transitEvidence  `json:"transit_evidence,omitempty"`
	StartPose       *transitPose      `json:"start_pose,omitempty"`
	StartInterval   *routeInterval    `json:"start_interval,omitempty"`
	EndInterval     *routeInterval    `json:"end_interval,omitempty"`
	EndPose         *transitPose      `json:"end_pose,omitempty"`
}

type routeRecording struct {
	fixedRoute
}

type routePlayback struct {
	Trial         bool
	TrialGestures []trialGesture
	Route         fixedRoute
	Probe         bool
	Next          int
	Started       time.Time
	LastCompleted time.Time
	Alignments    int
	AlignError    float64
	AlignStarted  time.Time
	WorldProbe    bool
	LoadedProbe   bool
	TrayBefore    map[string]int
}

// Route state contains only plain data. Framework handles never cross callbacks.
type fixedRouteState struct {
	Journey     []fixedRoute
	Waypoint    string
	Routes      []fixedRoute
	Recording   *routeRecording
	Playback    *routePlayback
	Seat        string
	BeforeServe []SeatView
}

func loadFixedRoutes() ([]fixedRoute, error) {
	data, err := os.ReadFile(fsutil.OutputPath("data", "Innkeeping", "fixed-routes.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var routes []fixedRoute
	err = json.Unmarshal(data, &routes)
	return routes, err
}

func validFixedRoute(r fixedRoute) bool {
	if r.Goal == "transit" {
		return validTransitRoute(r)
	}
	if (r.CounterHub && r.Goal != "counter") || (r.SeatMarker != [4]int{} && (r.Goal != "diner" || !validSeatMarker(r.SeatMarker))) {
		return false
	}
	if r.StartSeatMarker != [4]int{} && (!r.CounterHub || !validSeatMarker(r.StartSeatMarker)) {
		return false
	}
	if r.Version != 1 || r.ID == "" || (r.Goal != "diner" && r.Goal != "counter") || len(r.Seat) != 1 || r.Seat < "1" || r.Seat > "6" || len(r.Steps) == 0 || len(r.Steps) > 24 || !sceneMatches(r.Start, r.Start) || !sceneMatches(r.End, r.End) {
		return false
	}
	total := 0
	for _, s := range r.Steps {
		if s.Kind == "camera" || !validJoystickWarmup(s) || s.Milliseconds <= 0 || s.HoldMilliseconds < 0 || movementDurationMS(s) > 1500 || s.X < 100 || s.X > 290 || s.Y < 450 || s.Y > 640 {
			return false
		}
		total += movementDurationMS(s)
	}
	return total <= 12000
}

func (s *liveSession) rememberStep(img image.Image, cmd movementCommand) {
	// Camera searches cannot define reusable fixed-camera paths. Start a new
	// recording only at a confirmed pickup or serve, never halfway through a leg.
	if s.Fixed.Recording == nil {
		return
	}
	r := s.Fixed.Recording
	if cmd.Kind == "camera" || len(r.Steps) >= 24 || r.Goal != cmd.Goal {
		s.Fixed.Recording = nil
		return
	}
	if len(r.Steps) == 0 {
		r.Start = fingerprint(img)
	}
	r.Steps = append(r.Steps, cmd)
}

// captureRouteEnd runs before the validated interaction; the matching action
// acknowledgement, not reaching a prompt alone, authorizes saving the path.
func (s *liveSession) captureRouteEnd(img image.Image, v LiveView, kind string) {
	if kind == "serve" {
		s.Fixed.BeforeServe = append([]SeatView(nil), v.Seats...)
	}
	if r := s.Fixed.Recording; r != nil && ((kind == "serve" && r.Goal == "diner") || (kind == "pickup" && r.Goal == "counter")) {
		r.End = fingerprint(img)
	} else if kind != "select_tray" {
		s.Fixed.Recording = nil
	}
}

func servedSeat(before, after []SeatView, dish string) string {
	seat := ""
	for _, b := range before {
		for _, a := range after {
			if a.Seat != b.Seat {
				continue
			}
			count := func(ids []string) int {
				n := 0
				for _, id := range ids {
					if id == dish {
						n++
					}
				}
				return n
			}
			if b.Phase == "waiting" && count(b.Dishes)-count(a.Dishes) == 1 && (a.Phase == "dining" || a.Phase == "waiting") {
				// Partial orders must preserve every other food request. A new
				// diner with a different order cannot certify this destination.
				otherBefore, otherAfter := map[string]int{}, map[string]int{}
				for _, id := range b.Dishes {
					if id != dish {
						otherBefore[id]++
					}
				}
				for _, id := range a.Dishes {
					if id != dish {
						otherAfter[id]++
					}
				}
				if len(otherBefore) != len(otherAfter) {
					return ""
				}
				for id, n := range otherBefore {
					if otherAfter[id] != n {
						return ""
					}
				}
				if seat != "" {
					return ""
				}
				seat = a.Seat
			}
		}
	}
	return seat
}

func (s *liveSession) confirmRoute(kind, dish string, v LiveView) error {
	if kind != "pickup" && kind != "serve" {
		return nil
	}
	r := s.Fixed.Recording
	seat := s.Fixed.Seat
	if kind == "serve" {
		seat = servedSeat(s.Fixed.BeforeServe, v.Seats, dish)
		s.Fixed.Seat = seat
		s.Fixed.BeforeServe = nil
	}
	if r != nil {
		if kind == "serve" {
			r.Seat = seat
		}
		if validFixedRoute(r.fixedRoute) {
			if err := s.saveFixedRoute(r.fixedRoute); err != nil {
				return err
			}
		}
	}
	goal := "diner"
	if kind == "serve" {
		goal = "counter"
	}
	s.Fixed.Recording = &routeRecording{fixedRoute: fixedRoute{Version: 1, ID: fmt.Sprintf("%s-%d", goal, time.Now().UnixNano()), Goal: goal, Seat: seat, CreatedAt: time.Now()}}
	return nil
}

func (s *liveSession) saveFixedRoute(r fixedRoute) error {
	updated := false
	for i := range s.Fixed.Routes {
		if s.Fixed.Routes[i].ID == r.ID {
			s.Fixed.Routes[i] = r
			updated = true
			break
		}
	}
	if !updated {
		s.Fixed.Routes = append(s.Fixed.Routes, r)
	}
	if len(s.Fixed.Routes) > 96 {
		s.Fixed.Routes = s.Fixed.Routes[len(s.Fixed.Routes)-96:]
	}
	data, err := json.MarshalIndent(s.Fixed.Routes, "", "    ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(fsutil.OutputPath("data", "Innkeeping", "fixed-routes.json"), data, 0644)
}

// prepareRouteProbe validates a recorded path in an existing Trial without
// starting another session, cooking, or clicking any interaction at its end.
func (s *liveSession) prepareRouteProbe(ctx *maa.Context, id string, img image.Image, v LiveView) bool {
	if (v.Phase != "active" && v.Phase != "closing" && v.Phase != "closed_serving" && v.Phase != "world") || len(v.Unknown) != 0 {
		return false
	}
	for _, r := range s.Fixed.Routes {
		if r.ID != id || !validFixedRoute(r) {
			continue
		}
		if r.Goal == "transit" {
			if !transitDepartureMatches(ctx, img, r) {
				continue
			}
		} else if !routeDepartureNearby(img, r) {
			continue
		}
		if v.Phase == "world" && r.Goal != "transit" {
			return false
		}
		if r.Goal == "counter" && len(v.Tray) != 0 {
			return false
		}
		if r.Goal == "diner" && len(v.Tray) == 0 && !r.CalibrationOnly {
			return false
		}
		if r.Goal == "diner" && len(v.Tray) > 0 {
			compatible := false
			if len(v.Tray) == 1 && v.SelectedSlot >= 0 && v.SelectedSlot < len(v.TraySlots) {
				for _, seat := range v.Seats {
					if seat.Seat == r.Seat && includes(seat.Dishes, v.TraySlots[v.SelectedSlot]) {
						compatible = true
					}
				}
			}
			if !compatible {
				return false
			}
		}
		s.Fixed.Playback = &routePlayback{Route: r, Probe: true, WorldProbe: v.Phase == "world", LoadedProbe: trayCount(v.Tray) > 0, TrayBefore: maps.Clone(v.Tray), Started: time.Now(), LastCompleted: time.Now()}
		s.Fixed.Recording = &routeRecording{fixedRoute: r}
		s.Started = true
		return true
	}
	return false
}

func (s *liveSession) startFixedRoute(img image.Image, v LiveView) bool {
	held := 0
	for _, count := range v.Tray {
		held += count
	}
	if held > 1 {
		return false
	}
	goal := "counter"
	if len(v.Tray) == 1 {
		goal = "diner"
	}
	for _, r := range s.Fixed.Routes {
		// A captured path is a candidate until its fast replay reaches the
		// expected interaction. Manual pauses are not part of the saved gestures.
		if r.CalibrationOnly || r.Replays < 1 || !validFixedRoute(r) || r.Goal != goal || !routeDepartureNearby(img, r) {
			continue
		}
		if goal == "counter" && s.Fixed.Seat != r.Seat {
			continue
		}
		compatible := goal == "counter" && len(collectibleDishes(s.Jobs, v.Seats, time.Now())) > 0
		if goal == "diner" {
			for _, seat := range v.Seats {
				if seat.Seat == r.Seat {
					for dish, n := range v.Tray {
						if n == 1 && includes(seat.Dishes, dish) {
							compatible = true
						}
					}
				}
			}
		}
		if !compatible {
			continue
		}
		s.Fixed.Playback = &routePlayback{Route: r, Started: time.Now(), LastCompleted: time.Now()}
		s.Fixed.Recording = &routeRecording{fixedRoute: r}
		s.Fixed.Recording.Steps = nil
		return true
	}
	return false
}

var replayAgentIdentity struct {
	once sync.Once
	hash string
}

func replayAgentHash() string {
	replayAgentIdentity.once.Do(func() {
		executable, err := os.Executable()
		if err != nil {
			return
		}
		data, err := os.ReadFile(executable)
		if err == nil {
			replayAgentIdentity.hash = fmt.Sprintf("%x", sha256.Sum256(data))
		}
	})
	return replayAgentIdentity.hash
}

func calibrationRouteHash(r fixedRoute) string {
	// Replay counters change during validation; geometry and inputs must not.
	r.ID = ""
	r.CreatedAt = time.Time{}
	r.Replays, r.InnReplays, r.LoadedReplays, r.EmptyReplays = 0, 0, 0, 0
	r.Evidence = nil
	data, _ := json.Marshal(r)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func traceFixedRoute(p *routePlayback, stage string) error {
	f, err := os.OpenFile(fsutil.OutputPath("data", "Innkeeping", "route-replays.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	mode := "unknown"
	if p.WorldProbe {
		mode = "world"
	} else if p.TrayBefore != nil {
		mode = "empty"
		if trayCount(p.TrayBefore) > 0 {
			mode = "loaded"
		}
	} else if p.Probe {
		mode = "empty"
		if p.LoadedProbe {
			mode = "loaded"
		}
	}
	return json.NewEncoder(f).Encode(struct {
		Attempt   string    `json:"attempt_id"`
		AgentHash string    `json:"agent_sha256"`
		RouteHash string    `json:"route_sha256"`
		Mode      string    `json:"mode"`
		At        time.Time `json:"at"`
		Route     string    `json:"route"`
		Seat      string    `json:"seat"`
		Stage     string    `json:"stage"`
		Completed int       `json:"completed_segments"`
		Total     int       `json:"total_segments"`
		Elapsed   float64   `json:"elapsed_seconds"`
	}{p.Started.UTC().Format(time.RFC3339Nano), replayAgentHash(), calibrationRouteHash(p.Route), mode,
		time.Now(), p.Route.ID, p.Route.Seat, stage, p.Next, len(p.Route.Steps), time.Since(p.Started).Seconds()})
}

func (s *liveSession) departFixedRoute(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if s.Fixed.Playback == nil {
		return false
	}
	if err := ctx.OverridePipeline(map[string]any{"InnkeepingFixedRouteDepart": map[string]any{
		"pre_wait_freezes": map[string]any{"target": s.Fixed.Playback.Route.Start[0].Box, "time": 250, "threshold": .99, "timeout": 1500},
	}}); err != nil {
		return false
	}
	return route(ctx, arg, "InnkeepingFixedRouteDepart")
}

var _ maa.CustomActionRunner = &InnFixedRouteStepAction{}

// InnFixedRouteStepAction sends one recorded swipe. Pipeline repeats this node
// without full OCR between segments; departure and arrival remain visual gates.
type InnFixedRouteStepAction struct{ owner *ReactiveAction }

func (a *InnFixedRouteStepAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || a.owner == nil {
		return false
	}
	a.owner.mu.Lock()
	defer a.owner.mu.Unlock()
	s := a.owner.sessions[arg.TaskID]
	if s == nil || s.Fixed.Playback == nil {
		return false
	}
	p := s.Fixed.Playback
	if p.Next > 0 && p.Next < len(p.Route.Steps) && time.Since(p.LastCompleted) > 2*time.Second {
		_ = traceFixedRoute(p, "interrupted")
		s.announce(ctx, "Fixed inn route interrupted; reposition at its start before restarting.")
		return false
	}
	t := ctx.GetTasker()
	if t == nil {
		return false
	}
	c := t.GetController()
	if c == nil {
		return false
	}
	if p.Next < len(p.Route.Steps) {
		if p.Next == 0 {
			if !p.AlignStarted.IsZero() && time.Since(p.AlignStarted) > 4*time.Second {
				_ = traceFixedRoute(p, "alignment_deadline")
				return false
			}
			c.PostScreencap().Wait()
			img, err := c.CacheImage()
			if err != nil || img == nil {
				return false
			}
			startMatch := sceneMatchesImage(img, p.Route.Start)
			if p.Route.Goal == "transit" {
				startMatch = transitRoutePoseMatches(ctx, img, p.Route, false)
			}
			if !startMatch && p.Route.CounterHub && validSeatMarker(p.Route.StartSeatMarker) {
				startMatch = readPhysicalSeatMarker(ctx, img, p.Route.Start, p.Route.StartSeatMarker, p.Route.Seat, p.Route.SeatTemplate)
			}
			if !startMatch {
				cmd, distance, ok := planRouteAlignment(img, p.Route)
				if ok && p.Alignments < 2 && (p.AlignStarted.IsZero() || time.Since(p.AlignStarted) <= 4*time.Second) && (p.Alignments == 0 || distance < p.AlignError-1) {
					if p.AlignStarted.IsZero() {
						p.AlignStarted = time.Now()
					}
					if !issueMovement(ctx, c, cmd) {
						return false
					}
					p.Alignments++
					p.AlignError = distance
					_ = traceFixedRoute(p, "departure_alignment")
					return s.departFixedRoute(ctx, arg)
				}
				_ = traceFixedRoute(p, "start_mismatch")
				s.announce(ctx, "Inn route start changed; no recorded movement sent.")
				return false
			}
			if !p.AlignStarted.IsZero() && time.Since(p.AlignStarted) > 4*time.Second {
				_ = traceFixedRoute(p, "alignment_deadline")
				return false
			}
			if err = traceFixedRoute(p, "started"); err != nil {
				return false
			}
		}
		cmd := p.Route.Steps[p.Next]
		var boundary string
		if p.Trial {
			var err error
			boundary, err = s.recordTrialBoundary(c, "trial-before")
			if err != nil {
				return false
			}
		}
		if !issueMovement(ctx, c, cmd) {
			_ = traceFixedRoute(p, "controller_failed")
			return false
		}
		if p.Trial {
			after, err := s.recordTrialBoundary(c, "trial-after")
			if err != nil {
				return false
			}
			p.TrialGestures = append(p.TrialGestures, trialGesture{Label: fmt.Sprintf("%s-%d-step%d", p.Route.ID, p.Started.UnixNano(), p.Next), At: time.Now(), Steps: []movementCommand{cmd}, Before: boundary, After: after, TaskIDs: []int64{arg.TaskID}})
		}
		p.Next++
		p.LastCompleted = time.Now()
		if p.Next == len(p.Route.Steps) {
			if err := ctx.OverridePipeline(map[string]any{"InnkeepingFixedRouteArrive": map[string]any{"pre_wait_freezes": map[string]any{"target": p.Route.End[0].Box, "time": 150, "threshold": .99, "timeout": 1500}}}); err != nil {
				return false
			}
			return route(ctx, arg, "InnkeepingFixedRouteArrive")
		}
		return route(ctx, arg, "InnkeepingFixedRouteStep")
	}
	c.PostScreencap().Wait()
	img, err := c.CacheImage()
	if err != nil || img == nil {
		return false
	}
	tokens, err := screenocr.Read(ctx, img)
	if err != nil {
		return false
	}
	v := ReadLive(img, tokens, s.Icons, s.Catalog)
	sceneMatch := sceneMatchesImage(img, p.Route.End)
	geometryMatch := sceneMatch
	seatMarkerMatch := false
	if p.Route.Goal == "diner" && validSeatMarker(p.Route.SeatMarker) && p.Route.SeatTemplate != "" {
		seatMarkerMatch = readPhysicalSeatMarker(ctx, img, p.Route.End, p.Route.SeatMarker, p.Route.Seat, p.Route.SeatTemplate)
		geometryMatch = seatMarkerMatch
	}
	if p.Route.Goal != "transit" && (p.Route.CounterHub || (validSeatMarker(p.Route.SeatMarker) && p.Route.SeatTemplate == "")) {
		offsets, nearby := locateRouteScene(img, p.Route.End)
		if p.Route.CounterHub && p.Route.Goal == "counter" {
			// This reviewed route ends at a counter staging point. Observe must
			// still recognize and validate the actual ready-food pickup afterward.
			geometryMatch = nearby
		} else if p.Route.Goal == "diner" && nearby {
			box := shiftedSeatMarker(p.Route.SeatMarker, offsets)
			markerTokens, e := screenocr.ReadRegion(ctx, img, maa.Rect(box), true)
			seatMarkerMatch = e == nil && confirmsSeatMarker(markerTokens, p.Route.Seat)
			geometryMatch = seatMarkerMatch
		} else {
			geometryMatch = false
		}
	}
	log.Info().Str("route", p.Route.ID).Bool("scene_match", sceneMatch).Bool("geometry_match", geometryMatch).Bool("seat_marker_match", seatMarkerMatch).Str("phase", v.Phase).Strs("unknown", v.Unknown).Str("prompt", v.Prompt).Bool("probe", p.Probe).Msg("inn fixed route endpoint check")
	arrived := geometryMatch && (v.Phase == "active" || v.Phase == "closing" || v.Phase == "closed_serving") && len(v.Unknown) == 0
	if p.Route.Goal == "transit" {
		// Transit proves a physical pose, independently of tray contents and
		// nearby interaction labels. Observe revalidates the order and Serve.
		arrived = transitRoutePoseMatches(ctx, img, p.Route, true) && transitArrivalValid(*p, v)
		if p.Route.SeatMarker != [4]int{} {
			arrived = arrived && readPhysicalSeatMarker(ctx, img, p.Route.End, p.Route.SeatMarker, p.Route.Seat, p.Route.SeatTemplate)
		}
	} else if p.Probe && len(v.Tray) == 0 && p.Route.CalibrationOnly {
		// Geometry-only calibration ends at the recorded architectural pose;
		// production serving still requires the matching dish and green control.
	} else if p.Route.Goal == "diner" {
		arrived = arrived && v.Prompt == "serve" && greenServeControl(img, v.PromptBox)
		compatible := false
		if len(v.Tray) == 1 && v.SelectedSlot >= 0 && v.SelectedSlot < len(v.TraySlots) {
			for _, seat := range v.Seats {
				if seat.Seat == p.Route.Seat && includes(seat.Dishes, v.TraySlots[v.SelectedSlot]) {
					compatible = true
				}
			}
		}
		arrived = arrived && compatible
	} else if p.Route.CounterHub {
		arrived = arrived && len(v.Tray) == 0
	} else {
		arrived = arrived && len(v.Tray) == 0 && v.Prompt == "pickup" && (p.Probe || collectibleDishes(s.Jobs, v.Seats, time.Now())[v.PromptDish])
	}
	if p.Trial {
		arrived = arrived && maps.Equal(p.TrayBefore, v.Tray)
	}
	if !arrived {
		_ = traceFixedRoute(p, "endpoint_mismatch")
		_ = s.evidence(time.Now(), img, "fixed-route-endpoint-mismatch")
		s.announce(ctx, "Fixed inn route endpoint did not match; stopped without repeating movement.")
		return false
	}
	if err = traceFixedRoute(p, "arrived"); err != nil {
		return false
	}
	if err = s.evidence(time.Now(), img, "fixed-route-arrived"); err != nil {
		return false
	}
	if p.Trial {
		if err := s.saveTrialGestures(p, arg.TaskID); err != nil {
			return false
		}
		// Research execution records no production replay credit. Native pickup and
		// Serve remain in ReactiveObserve and its existing Pending acknowledgement.
		if p.Route.Goal == "diner" {
			s.acknowledgeTransitEndpoint(fixedRoute{Seat: p.Route.Seat}, time.Now())
		} else {
			s.acknowledgeTransitEndpoint(fixedRoute{CounterHub: true}, time.Now())
		}
		s.Fixed.Playback, s.Fixed.Recording = nil, nil
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	if p.Route.Goal == "transit" {
		if p.Probe {
			verified := p.Route
			verified.Replays++
			if !p.WorldProbe {
				verified.InnReplays++
				if p.LoadedProbe {
					verified.LoadedReplays++
				} else {
					verified.EmptyReplays++
				}
			}
			if err = s.saveFixedRoute(verified); err != nil {
				return false
			}
		}
		s.Fixed.Playback = nil
		s.Fixed.Recording = nil
		if p.Probe {
			return route(ctx, arg, "InnkeepingReactiveDone")
		}
		s.acknowledgeTransitEndpoint(p.Route, time.Now())
		if s.continueTransit(p.Route, v) {
			return s.departFixedRoute(ctx, arg)
		}
		if len(s.Fixed.Journey) > 0 {
			s.announce(ctx, "Saved inn path continuity changed; stopped before further movement.")
			return false
		}
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	s.Fixed.Recording.Steps = append([]movementCommand(nil), p.Route.Steps...)
	s.Fixed.Recording.Replays++
	if p.Probe {
		if err = s.saveFixedRoute(s.Fixed.Recording.fixedRoute); err != nil {
			return false
		}
	}
	s.Fixed.Playback = nil
	if p.Probe {
		return route(ctx, arg, "InnkeepingReactiveDone")
	}
	return route(ctx, arg, "InnkeepingReactiveObserve")
}
