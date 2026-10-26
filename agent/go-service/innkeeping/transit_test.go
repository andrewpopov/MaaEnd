package innkeeping

import (
	"fmt"
	"strconv"
	"testing"
	"time"
)

func TestLaneTravelCannotCutDiagonallyAcrossTables(t *testing.T) {
	r := transitFixture(t)[0]
	r.MovementStage = "lane"
	r.Lane = "outer-right"
	if !validTransitRoute(r) {
		t.Fatal("recorded straight lane rejected")
	}
	r.Steps[0].X = 250
	if validTransitRoute(r) {
		t.Fatal("diagonal table shortcut admitted as lane travel")
	}
	r.Steps[0].X = 195
	r.Steps[0].Kind = "camera"
	if validTransitRoute(r) {
		t.Fatal("camera pan admitted during lane travel")
	}
	r.Steps[0] = movementCommand{Kind: "walk", X: 115, Y: 551, Milliseconds: 20, HoldMilliseconds: 700, Pipeline: true}
	r.Lane = "front-cross"
	if !validTransitRoute(r) {
		t.Fatal("recorded transverse aisle rejected")
	}
	r.MovementStage = "approach"
	r.Lane = ""
	r.Steps = append(r.Steps, r.Steps[0], r.Steps[0])
	if validTransitRoute(r) {
		t.Fatal("long travel disguised as table approach")
	}
}

func transitFixture(t *testing.T) []fixedRoute {
	t.Helper()
	a := fingerprint(navImage(t, "inn-fin-held"))
	b := fingerprint(navImage(t, "inn-table-two-approach"))
	// Two directed edges, with one canonical aisle pose shared by both.
	edge := func(id, from, to string, start, end sceneFingerprint) fixedRoute {
		return fixedRoute{Version: 1, ID: id, Goal: "transit", FromWaypoint: from, ToWaypoint: to, Start: start, End: end, Replays: 1, InnReplays: 2, LoadedReplays: 1, EmptyReplays: 1,
			Steps: []movementCommand{{Kind: "walk", X: 195, Y: 471, Milliseconds: 20, HoldMilliseconds: 450, Pipeline: true}}}
	}
	first := edge("counter-aisle", "counter", "aisle", a, b)
	first.MovementStage, first.Lane = "lane", "inner"
	second := edge("aisle-table4", "aisle", "table4", b, a)
	second.MovementStage = "approach"
	second.Seat = "4"
	return []fixedRoute{first, second}
}

func TestLoadedAndEmptyTravelRequireIndependentReplays(t *testing.T) {
	r := transitFixture(t)[0]
	r.EmptyReplays = 0
	if productionTransit(r, LiveView{Tray: map[string]int{}}) {
		t.Fatal("loaded speed authorized empty travel")
	}
	if !productionTransit(r, LiveView{Tray: map[string]int{"congee": 1}}) {
		t.Fatal("verified loaded travel rejected")
	}
	r.LoadedReplays = 0
	r.EmptyReplays = 1
	if productionTransit(r, LiveView{Tray: map[string]int{"congee": 1}}) {
		t.Fatal("empty speed authorized loaded travel")
	}
	if !productionTransit(r, LiveView{Tray: map[string]int{}}) {
		t.Fatal("verified empty travel rejected")
	}
}

func TestProductionTransitUsesInsideAisleEvenWhenOuterLaneWasReplayed(t *testing.T) {
	r := transitFixture(t)[0]
	r.MovementStage, r.Lane = "", ""
	if productionTransit(r, LiveView{Tray: map[string]int{"congee": 1}}) {
		t.Fatal("unclassified route bypassed the inside-aisle policy")
	}
	r.MovementStage = "lane"
	v := LiveView{Tray: map[string]int{"congee": 1}}
	for _, lane := range []string{"outer-right", "outer-left"} {
		r.Lane = lane
		if !validTransitRoute(r) || productionTransit(r, v) {
			t.Fatal("valid outer recording became a production shortcut", lane)
		}
	}
	r.Lane = "inner"
	if !productionTransit(r, v) {
		t.Fatal("verified inside aisle rejected")
	}
}

func TestTransitNavigatesBatchToHUDDestinationThroughAisle(t *testing.T) {
	routes := transitFixture(t)
	v := LiveView{Phase: "active", Tray: map[string]int{"congee": 1, "cracker": 2, "shroom": 1}, Seats: []SeatView{{Seat: "4", Dishes: []string{"congee", "cracker"}}}}
	edge, ok := planTransit(routes, []string{"counter"}, v, false)
	if !ok || edge.ID != "counter-aisle" {
		t.Fatalf("four-item tray must travel through its measured aisle edge: %+v %v", edge, ok)
	}
	edge, ok = planTransit(routes, []string{"aisle"}, v, false)
	if !ok || edge.ID != "aisle-table4" {
		t.Fatalf("physical table destination must not require a floating food bubble: %+v %v", edge, ok)
	}
	// A table whose order no longer matches the tray is no longer a destination.
	v.Seats[0].Dishes = []string{"fin"}
	if _, ok := planTransit(routes, []string{"counter"}, v, false); ok {
		t.Fatal("stale or incompatible table demand authorized travel")
	}
}

func TestTransitRequiresMeasuredDirectionReplayAndCanonicalPose(t *testing.T) {
	routes := transitFixture(t)
	v := LiveView{Tray: map[string]int{"cracker": 1}, Seats: []SeatView{{Seat: "4", Dishes: []string{"cracker"}}}}
	routes[1].Replays = 0
	if _, ok := planTransit(routes, []string{"counter"}, v, false); ok {
		t.Fatal("unreplayed edge became production navigation")
	}
	routes[1].Replays = 1
	routes[0].CounterHub = true // Counter endpoint is still not a reverse edge.
	if _, ok := planTransit(routes, []string{"table4"}, v, true); ok {
		t.Fatal("reverse gestures or table adjacency must never be inferred")
	}
	routes[1].Start = routes[0].Start
	if _, ok := planTransit(routes, []string{"counter"}, v, false); ok {
		t.Fatal("same waypoint with a different camera pose must reject the graph")
	}
}

func TestTransitSelectionRequiresCurrentPhysicalPose(t *testing.T) {
	routes := transitFixture(t)
	v := LiveView{Phase: "active", Tray: map[string]int{"cracker": 2}, Seats: []SeatView{{Seat: "4", Dishes: []string{"cracker"}}}}
	s := &liveSession{Fixed: fixedRouteState{Routes: routes}}
	if !s.startTransitRoute(nil, navImage(t, "inn-fin-held"), v, false) {
		t.Fatal("verified departure with a batch should select transit")
	}
	s.Fixed.Playback = nil
	if s.startTransitRoute(nil, navImage(t, "inn-menu-drinks"), v, false) {
		t.Fatal("unlocalized scene must not send a stored route")
	}
}

func TestWorldOrEmptyTrayReplayDoesNotAuthorizeLoadedTravel(t *testing.T) {
	r := transitFixture(t)[0]
	v := LiveView{Tray: map[string]int{"cracker": 2}}
	r.InnReplays, r.LoadedReplays = 0, 0
	if productionTransit(r, v) {
		t.Fatal("world geometry cannot certify inn movement with a tray")
	}
	r.InnReplays = 1
	if productionTransit(r, v) {
		t.Fatal("empty-tray inn replay cannot certify a carrying route")
	}
	r.LoadedReplays = 1
	if !productionTransit(r, v) {
		t.Fatal("loaded inn replay should certify the measured route")
	}
}

func TestTransitArrivalPreservesPhaseFamilyAndInventory(t *testing.T) {
	p := routePlayback{Probe: true, LoadedProbe: true, TrayBefore: map[string]int{"cracker": 2}}
	v := LiveView{Phase: "active", Tray: map[string]int{"cracker": 2}}
	if !transitArrivalValid(p, v) {
		t.Fatal("loaded inn arrival with unchanged inventory rejected")
	}
	v.Phase = "world"
	if transitArrivalValid(p, v) {
		t.Fatal("leaving the inn must not certify a loaded inn replay")
	}
	p.WorldProbe = true
	if !transitArrivalValid(p, v) {
		t.Fatal("world probe must verify its world endpoint")
	}
	v.Phase = "active"
	if transitArrivalValid(p, v) {
		t.Fatal("world probe entering an inn session must not certify world geometry")
	}
	p.WorldProbe = false
	v.Tray["cracker"] = 1
	if transitArrivalValid(p, v) {
		t.Fatal("a removed or served dish must not certify transit-only movement")
	}
}

func TestConnectedTransitEdgesShareTheirPhysicalPoseContract(t *testing.T) {
	routes := transitFixture(t)
	routes[0].EndPose = &transitPose{Number: "1", Box: [4]int{548, 305, 30, 42}, Template: "one.png"}
	routes[1].StartPose = &transitPose{Number: "5", Box: [4]int{548, 305, 30, 42}, Template: "five.png"}
	v := LiveView{Tray: map[string]int{"cracker": 2}, Seats: []SeatView{{Seat: "4", Dishes: []string{"cracker"}}}}
	if _, ok := planTransit(routes, []string{"counter"}, v, false); ok {
		t.Fatal("a graph must not join incompatible arrival and departure pose gates")
	}
}

func TestTransitRejectsInvalidEndpointIntervals(t *testing.T) {
	r := transitFixture(t)[0]
	r.StartInterval = &routeInterval{}
	if validTransitRoute(r) {
		t.Fatal("invalid departure interval accepted")
	}
	r.StartInterval = nil
	r.EndInterval = &routeInterval{}
	if validTransitRoute(r) {
		t.Fatal("invalid arrival interval accepted")
	}
}

func TestTransitJourneyRetainsEveryMeasuredEdge(t *testing.T) {
	routes := transitFixture(t)
	v := LiveView{Phase: "active", Tray: map[string]int{"cracker": 1}, Seats: []SeatView{{Seat: "4", Dishes: []string{"cracker"}}}}
	path, ok := planTransitJourney(routes, []string{"counter"}, v, false)
	if !ok || len(path) != 2 || path[0].ID != "counter-aisle" || path[1].ID != "aisle-table4" {
		t.Fatalf("lost complete journey: %+v", path)
	}
	s := &liveSession{Fixed: fixedRouteState{Journey: path[1:]}}
	if !s.continueTransit(path[0], v) || s.Fixed.Playback.Route.ID != "aisle-table4" || s.Fixed.Waypoint != "aisle" || len(s.Fixed.Journey) != 0 {
		t.Fatal("intermediate arrival discarded selected table approach")
	}
	if s.continueTransit(path[1], v) || s.Fixed.Waypoint != "table4" {
		t.Fatal("service endpoint must return to fresh interaction observation")
	}
}

func TestTransitJourneyRejectsBrokenOrUnverifiedContinuation(t *testing.T) {
	routes := transitFixture(t)
	v := LiveView{Phase: "active", Tray: map[string]int{"cracker": 1}}
	for _, broken := range []bool{true, false} {
		edge := routes[1]
		if broken {
			edge.FromWaypoint = "other"
		} else {
			edge.LoadedReplays = 0
		}
		s := &liveSession{Fixed: fixedRouteState{Journey: []fixedRoute{edge}}}
		if s.continueTransit(routes[0], v) || s.Fixed.Playback != nil || len(s.Fixed.Journey) != 1 {
			t.Fatal("invalid continuation authorized movement")
		}
	}
}

func TestAcknowledgedDinerTripKeepsDepartureAndEmptyReturnIntent(t *testing.T) {
	now := time.Unix(1000, 0)
	s := liveSession{Navigation: navigationState{Goal: "counter"}, Jobs: []cookJob{{Dish: "cake", Phase: "ready"}}}
	v := LiveView{Tray: map[string]int{"soup": 1}, Seats: []SeatView{{Dishes: []string{"soup", "cake"}}}}
	if !s.collectingBatch(v, now) {
		t.Fatal("counter should collect useful ready food before departure")
	}
	s.acknowledgeTransitEndpoint(fixedRoute{Seat: "1"}, now)
	if s.collectingBatch(v, now) {
		t.Fatal("newly due food reversed acknowledged diner departure")
	}
	s.afterServed(now.Add(time.Second))
	v.Tray = map[string]int{}
	s.Jobs = nil
	if !s.needsCounterReturn(v) {
		t.Fatal("empty diner return incorrectly depends on ready cooking")
	}
	s.acknowledgeTransitEndpoint(fixedRoute{CounterHub: true}, now.Add(2*time.Second))
	if s.needsCounterReturn(v) || s.Navigation.Departed {
		t.Fatal("counter arrival retained a completed delivery leg")
	}
}

func TestTransitCompletesReachableMealBeforeCheaperPartialOrder(t *testing.T) {
	routes := transitFixture(t)
	complete := routes[1]
	complete.ID, complete.ToWaypoint, complete.Seat = "aisle-table2", "table2", "2"
	complete.Steps = []movementCommand{{Kind: "walk", X: 195, Y: 471, Milliseconds: 20, HoldMilliseconds: 1200, Pipeline: true}}
	routes = append(routes, complete)
	v := LiveView{Phase: "active", Tray: map[string]int{"cracker": 1, "soup": 1}, Seats: []SeatView{{Seat: "4", Dishes: []string{"cracker", "soup", "fin"}}, {Seat: "2", Dishes: []string{"cracker"}}}}
	journey, ok := planTransitJourney(routes, []string{"counter"}, v, false)
	if !ok || journey[len(journey)-1].Seat != "2" {
		t.Fatal("cheap partial meal defeated reachable completed customer", journey)
	}
	// Unsupported destinations cannot block service to a reachable customer.
	journey, ok = planTransitJourney(routes[:2], []string{"counter"}, v, false)
	if !ok || journey[len(journey)-1].Seat != "4" {
		t.Fatal("unreachable completed meal blocked reachable service", journey)
	}
}

func TestNativeServeAcknowledgementRequiresReturnWithoutGraphArrival(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, previous := range []navigationState{{}, {Goal: "counter", PickupZone: true}, {Goal: "diner", Departed: true}} {
		s := liveSession{Navigation: previous}
		s.afterServed(now)
		v := LiveView{Phase: "active", Tray: map[string]int{}}
		if !s.needsCounterReturn(v) {
			t.Fatalf("native service discarded diner position: %+v", s.Navigation)
		}
		state := ReactiveState{Phase: "active", Tray: v.Tray}
		decision := ReactiveDecision{Kind: "queue"}
		if s.Fixed.Waypoint != "" || len(s.Fixed.Routes) != 0 {
			t.Fatal("service invented route geometry")
		}
		s.acknowledgeTransitEndpoint(fixedRoute{CounterHub: true}, now.Add(time.Second))
		if s.needsCounterReturn(v) || !kitchenOpenRequired(state, decision, true) {
			t.Fatal("verified counter return did not release cooking")
		}
	}
}

func TestSixTableDeliveryChoosesNearestCompletableCustomer(t *testing.T) {
	routes := transitFixture(t)[:1]
	base := transitFixture(t)[1]
	v := LiveView{Phase: "active", Tray: map[string]int{"cracker": 1}}
	for seat := 1; seat <= 6; seat++ {
		edge := base
		edge.ID = fmt.Sprintf("aisle-table%d", seat)
		edge.ToWaypoint = fmt.Sprintf("table%d", seat)
		edge.Seat = strconv.Itoa(seat)
		edge.Steps = []movementCommand{{Kind: "walk", X: 195, Y: 471, Milliseconds: 20, HoldMilliseconds: 1400 - seat*150, Pipeline: true}}
		routes = append(routes, edge)
		v.Seats = append(v.Seats, SeatView{Seat: edge.Seat, Dishes: []string{"cracker"}})
	}
	journey, ok := planTransitJourney(routes, []string{"counter"}, v, false)
	if !ok || journey[len(journey)-1].Seat != "6" {
		t.Fatal("table number or shared-copy reservation defeated nearest completed meal", journey)
	}
	// After one acknowledged service removes the only physical copy, there is
	// no second delivery promise to any of the other five customers.
	v.Tray = map[string]int{}
	if _, ok := planTransitJourney(routes, []string{"counter"}, v, false); ok {
		t.Fatal("empty tray planned another customer's delivery")
	}
}

func TestCompletedCustomersCompeteByTravelNotDishCount(t *testing.T) {
	routes := transitFixture(t)
	near := routes[1]
	near.ID = "aisle-near"
	near.ToWaypoint = "near"
	near.Seat = "2"
	near.Steps = []movementCommand{{Kind: "walk", X: 195, Y: 471, Milliseconds: 20, HoldMilliseconds: 300, Pipeline: true}}
	routes[1].Steps = []movementCommand{{Kind: "walk", X: 195, Y: 471, Milliseconds: 20, HoldMilliseconds: 600, Pipeline: true}}
	routes = append(routes, near)
	v := LiveView{Phase: "active", Tray: map[string]int{"cracker": 1, "soup": 1, "fin": 1}, Seats: []SeatView{{Seat: "4", Dishes: []string{"cracker", "soup"}}, {Seat: "2", Dishes: []string{"fin"}}}}
	journey, ok := planTransitJourney(routes, []string{"counter"}, v, false)
	if !ok || journey[len(journey)-1].Seat != "2" {
		t.Fatal("food count defeated faster completed customer", journey)
	}
}
