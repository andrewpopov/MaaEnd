package innkeeping

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"image"
	"maps"
	"time"
)

type transitEvidence struct {
	StartImage string  `json:"start_image"`
	EndImage   string  `json:"end_image"`
	Journal    string  `json:"journal"`
	TaskIDs    []int64 `json:"task_ids"`
}

func productionTransit(r fixedRoute, v LiveView) bool {
	// Standardize travel on the inside aisle. Preserve outer-lane recordings
	// for inspection, but never select them as faster production shortcuts.
	if r.MovementStage == "" || (r.MovementStage == "lane" && r.Lane != "inner" && r.Lane != "front-cross") {
		return false
	}
	modeVerified := r.EmptyReplays > 0
	if trayCount(v.Tray) > 0 {
		modeVerified = r.LoadedReplays > 0
	}
	return r.Goal == "transit" && r.Replays > 0 && r.InnReplays > 0 && modeVerified && validTransitRoute(r)
}

func transitArrivalValid(p routePlayback, v LiveView) bool {
	if len(v.Unknown) > 0 || p.TrayBefore == nil || !maps.Equal(p.TrayBefore, v.Tray) {
		return false
	}
	if p.WorldProbe {
		return p.Probe && v.Phase == "world"
	}
	return v.Phase == "active" || v.Phase == "closing" || v.Phase == "closed_serving"
}

// Transit edges are measured directed gestures between canonical physical
// poses. They do not authorize picking up or serving any item.
func validTransitRoute(r fixedRoute) bool {
	if r.Version != 1 || r.ID == "" || r.FromWaypoint == "" || r.ToWaypoint == "" || r.FromWaypoint == r.ToWaypoint || r.Goal != "transit" || r.CalibrationOnly || len(r.Steps) == 0 || len(r.Steps) > 24 || !sceneMatches(r.Start, r.Start) || !sceneMatches(r.End, r.End) {
		return false
	}
	if r.Seat != "" && (len(r.Seat) != 1 || r.Seat < "1" || r.Seat > "6") {
		return false
	}
	if r.CounterHub && r.Seat != "" {
		return false
	}
	if r.SeatMarker != [4]int{} && (r.Seat == "" || !validSeatMarker(r.SeatMarker)) {
		return false
	}
	if (r.StartInterval != nil && !validRouteInterval(r.StartInterval)) || (r.EndInterval != nil && !validRouteInterval(r.EndInterval)) {
		return false
	}
	if (r.StartPose != nil && !validTransitPose(r.StartPose)) || (r.EndPose != nil && !validTransitPose(r.EndPose)) {
		return false
	}
	total := 0
	if r.MovementStage != "" && r.MovementStage != "lane" && r.MovementStage != "exit" && r.MovementStage != "approach" {
		return false
	}
	if r.MovementStage == "lane" && r.Lane != "inner" && r.Lane != "outer-right" && r.Lane != "outer-left" && r.Lane != "front-cross" {
		return false
	}
	for _, step := range r.Steps {
		if !validJoystickWarmup(step) || step.Milliseconds <= 0 || step.HoldMilliseconds < 0 || movementDurationMS(step) > 1500 || !step.Pipeline {
			return false
		}
		if r.MovementStage == "lane" {
			// Long travel follows a recorded aisle axis. Turning toward a table
			// belongs to a separate, verified approach edge at its opening.
			if step.Kind != "walk" {
				return false
			}
			if r.Lane == "front-cross" {
				if step.Y != 551 || (step.X != 115 && step.X != 275) {
					return false
				}
			} else if step.X != 195 || (step.Y != 471 && step.Y != 631) {
				return false
			}
		}
		if step.Kind == "camera" {
			if step.X < 500 || step.X > 1150 || step.Y < 150 || step.Y > 500 || step.HoldMilliseconds != 0 {
				return false
			}
			if step.Origin != [2]int{} && (step.Origin[0] < 500 || step.Origin[0] > 1150 || step.Origin[1] < 180 || step.Origin[1] > 500) {
				return false
			}
		} else if step.X < 100 || step.X > 290 || step.Y < 450 || step.Y > 640 {
			return false
		}
		total += movementDurationMS(step)
	}
	if (r.MovementStage == "exit" || r.MovementStage == "approach") && total > 1500 {
		return false
	}
	return total <= 12000
}

func transitCost(r fixedRoute) int {
	total := 0
	for _, step := range r.Steps {
		total += movementDurationMS(step)
	}
	return total + 500 // A fresh endpoint observation per edge.
}

// planTransit chooses a reachable physical destination from HUD demand and the
// complete tray. An edge is usable only after an independent successful replay.
// No reverse edge or numerical table adjacency is inferred.
func planTransit(routes []fixedRoute, starts []string, v LiveView, counter bool) (fixedRoute, bool) {
	journey, ok := planTransitJourney(routes, starts, v, counter)
	if !ok {
		return fixedRoute{}, false
	}
	return journey[0], true
}

// transitMealRanks compares alternative next customers against the same carried
// stock. Only one destination is committed, then stock is observed again after
// service. Reserving copies to table numbers here makes travel cost irrelevant.
func transitMealRanks(v LiveView) map[string]int {
	total := 0
	for _, seat := range v.Seats {
		total += len(seat.Dishes)
	}
	ranks := map[string]int{}
	for _, seat := range v.Seats {
		if len(seat.Dishes) == 0 {
			continue
		}
		needed := map[string]int{}
		for _, dish := range seat.Dishes {
			needed[dish]++
		}
		missing := 0
		for dish, n := range needed {
			missing += max(0, n-v.Tray[dish])
		}
		rank := missing * (total + 1)
		if missing > 0 {
			rank += len(seat.Dishes)
		}
		ranks[seat.Seat] = rank
	}
	return ranks
}

func planTransitJourney(routes []fixedRoute, starts []string, v LiveView, counter bool) ([]fixedRoute, bool) {
	weights := map[string]int{}
	priorities := map[string]int{}
	mealRanks := transitMealRanks(v)
	poses := map[string]sceneFingerprint{}
	gates := map[string]*transitPose{}
	for _, r := range routes {
		if !productionTransit(r, v) {
			continue
		}
		for id, pose := range map[string]sceneFingerprint{r.FromWaypoint: r.Start, r.ToWaypoint: r.End} {
			if existing, ok := poses[id]; ok && !sceneMatches(existing, pose) {
				// The same waypoint cannot silently mean two camera poses.
				return nil, false
			}
			poses[id] = pose
		}
		for id, gate := range map[string]*transitPose{r.FromWaypoint: r.StartPose, r.ToWaypoint: r.EndPose} {
			if previous, ok := gates[id]; ok && ((previous == nil) != (gate == nil) || (previous != nil && gate != nil && *previous != *gate)) {
				return nil, false
			}
			gates[id] = gate
		}
	}
	for _, r := range routes {
		if !productionTransit(r, v) {
			continue
		}
		if counter && r.CounterHub {
			weights[r.ToWaypoint] = 1
		} else if !counter && r.Seat != "" {
			weight := 0
			for _, seat := range v.Seats {
				if seat.Seat != r.Seat {
					continue
				}
				needed := map[string]int{}
				for _, dish := range seat.Dishes {
					needed[dish]++
				}
				for dish, count := range needed {
					weight += min(count, v.Tray[dish])
				}
			}
			weights[r.ToWaypoint] = max(weights[r.ToWaypoint], weight)
			priorities[r.ToWaypoint] = mealRanks[r.Seat]
		}
	}
	type path struct {
		cost  int
		edges []fixedRoute
	}
	paths := map[string]path{}
	visited := map[string]bool{}
	for _, start := range starts {
		paths[start] = path{}
	}
	for {
		node := ""
		for id, candidate := range paths {
			if !visited[id] && (node == "" || candidate.cost < paths[node].cost || (candidate.cost == paths[node].cost && id < node)) {
				node = id
			}
		}
		if node == "" {
			break
		}
		visited[node] = true
		for _, edge := range routes {
			if !productionTransit(edge, v) || edge.FromWaypoint != node {
				continue
			}
			candidate := path{cost: paths[node].cost + transitCost(edge), edges: append(append([]fixedRoute(nil), paths[node].edges...), edge)}
			if existing, ok := paths[edge.ToWaypoint]; !ok || candidate.cost < existing.cost {
				paths[edge.ToWaypoint] = candidate
			}
		}
	}
	var best []fixedRoute
	score := 0.0
	bestPriority := int(^uint(0) >> 1)
	for destination, weight := range weights {
		p, reachable := paths[destination]
		if !reachable || p.cost == 0 || weight <= 0 {
			continue
		}
		priority := priorities[destination]
		// A completed customer is one turnover, regardless of dish count.
		// Among complete meals choose the shortest verified trip.
		if !counter && priority == 0 {
			weight = 1
		}
		value := float64(weight) / float64(p.cost)
		if priority < bestPriority || priority == bestPriority && (value > score || value == score && (len(best) == 0 || p.edges[0].ID < best[0].ID)) {
			best, score, bestPriority = p.edges, value, priority
		}
	}
	return best, len(best) > 0
}

func (s *liveSession) startTransitRoute(ctx *maa.Context, img image.Image, v LiveView, counter bool) bool {
	starts := []string{}
	seen := map[string]bool{}
	for _, r := range s.Fixed.Routes {
		if productionTransit(r, v) && !seen[r.FromWaypoint] && transitDepartureMatches(ctx, img, r) {
			starts = append(starts, r.FromWaypoint)
			seen[r.FromWaypoint] = true
		}
	}
	journey, ok := planTransitJourney(s.Fixed.Routes, starts, v, counter)
	if !ok {
		return false
	}
	edge := journey[0]
	s.Fixed.Journey = append([]fixedRoute(nil), journey[1:]...)
	s.Fixed.Playback = &routePlayback{Route: edge, TrayBefore: maps.Clone(v.Tray), Started: time.Now(), LastCompleted: time.Now()}
	s.Fixed.Recording = &routeRecording{fixedRoute: edge}
	return true
}

// transitDepartureMatches selects a nearby calibrated start without authorizing
// its travel inputs. FixedRouteStep still corrects and verifies the exact pose
// before the first recorded step. Numbered-board poses retain their own gate.
func transitDepartureMatches(ctx *maa.Context, img image.Image, r fixedRoute) bool {
	if transitRoutePoseMatches(ctx, img, r, false) {
		return true
	}
	if r.StartPose != nil {
		return false
	}
	_, _, ok := planRouteAlignment(img, r)
	return ok
}

// transitRoutePoseMatches permits only the independently reviewed interval for
// this endpoint. Legacy routes keep their strict pose gate, and numbered-board
// recognition remains mandatory when present.
func transitRoutePoseMatches(ctx *maa.Context, img image.Image, r fixedRoute, arrival bool) bool {
	reference, pose, interval := r.Start, r.StartPose, r.StartInterval
	if arrival {
		reference, pose, interval = r.End, r.EndPose, r.EndInterval
	}
	if interval == nil {
		return transitPoseMatches(ctx, img, reference, pose)
	}
	if !routeIntervalMatches(img, reference, interval) {
		return false
	}
	return pose == nil || transitPoseMatches(ctx, img, reference, pose)
}

// continueTransit advances only through the already selected measured path.
// Pipeline executes each edge; its departure callback rechecks the live pose.
func (s *liveSession) continueTransit(arrived fixedRoute, v LiveView) bool {
	s.Fixed.Waypoint = arrived.ToWaypoint
	if len(s.Fixed.Journey) == 0 {
		return false
	}
	edge := s.Fixed.Journey[0]
	if edge.FromWaypoint != arrived.ToWaypoint || !productionTransit(edge, v) {
		return false
	}
	s.Fixed.Journey = s.Fixed.Journey[1:]
	s.Fixed.Playback = &routePlayback{Route: edge, TrayBefore: maps.Clone(v.Tray), Started: time.Now(), LastCompleted: time.Now()}
	s.Fixed.Recording = &routeRecording{fixedRoute: edge}
	return true
}

// acknowledgeTransitEndpoint updates travel intent only after a measured
// physical arrival. Pickup remains an independent native interaction.
func (s *liveSession) acknowledgeTransitEndpoint(arrived fixedRoute, now time.Time) {
	if arrived.CounterHub {
		s.Navigation = navigationState{Goal: "counter", Since: now, LegStarted: now}
	} else if arrived.Seat != "" {
		s.Navigation = navigationState{Goal: "diner", Departed: true, Since: now, LegStarted: now}
	}
}

func (s *liveSession) needsCounterReturn(v LiveView) bool {
	return trayCount(v.Tray) == 0 && s.Navigation.Goal == "diner" && s.Navigation.Departed
}

// afterServed records the native interaction's physical diner arrival even
// when movement came from a supervised macro rather than the verified graph.
// This grants return intent only; it never certifies an unmeasured route.
func (s *liveSession) afterServed(now time.Time) {
	s.Navigation = navigationState{Goal: "diner", Departed: true, Since: now, LegStarted: now}
}
