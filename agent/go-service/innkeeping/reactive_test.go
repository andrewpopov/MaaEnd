package innkeeping

import (
	"testing"
	"time"
)

func reactiveFixture() ReactiveState {
	return ReactiveState{Phase: "active", Verified: true, TrayCapacity: 3, RemainingSeconds: 120, Tray: map[string]int{}, Ready: map[string]int{}, Demands: []Demand{{ID: "seat1-food", Seat: "1", Allowed: []string{"cake"}, WaitingSeconds: 10}}, Dishes: []DishDefinition{{ID: "cake", Category: "dessert", Ingredients: map[string]int{"Amber Rice": 3}, IngredientsComplete: true}}, Kitchens: []ChefKitchen{{ID: "1", Chef: "Ember", Capacity: 3, Verified: true, Queued: map[string]int{}, Specialties: []string{"dessert"}, CookSeconds: map[string]float64{"cake": 14}, Available: map[string]bool{"cake": true}}, {ID: "2", Chef: "Da Pan", Capacity: 3, Verified: true, Queued: map[string]int{}, CookSeconds: map[string]float64{"cake": 15}, Available: map[string]bool{"cake": true}}}}
}
func TestReactiveChoosesChefAndChecksBothQueues(t *testing.T) {
	s := reactiveFixture()
	if d := PlanReactive(s); d.Kind != "queue" || d.Kitchen != "1" {
		t.Fatalf("expected specialist: %+v", d)
	}
	s.Kitchens[0].Queued = map[string]int{"other": 3}
	if d := PlanReactive(s); d.Kitchen != "2" {
		t.Fatalf("full first queue must not block second: %+v", d)
	}
	s.Kitchens[1].Queued = map[string]int{"other": 3}
	if PlanReactive(s).Kind != "wait" {
		t.Fatal("both full: do not repeat cook")
	}
}
func TestReactiveUsesCompletionTimeNotChefName(t *testing.T) {
	s := reactiveFixture()
	s.Kitchens[0].BusySeconds = 20
	if d := PlanReactive(s); d.Kitchen != "2" {
		t.Fatalf("idle generalist finishes before busy specialist: %+v", d)
	}
}
func TestReactiveDoesNotDuplicateQueuedDemand(t *testing.T) {
	s := reactiveFixture()
	s.Kitchens[1].Queued = map[string]int{"cake": 1}
	if PlanReactive(s).Kind != "wait" {
		t.Fatal("one order is already reserved")
	}
	s.Demands = append(s.Demands, Demand{ID: "seat2-food", Seat: "2", Allowed: []string{"cake"}})
	if PlanReactive(s).Kind != "queue" {
		t.Fatal("one queued cake must not cover two diners")
	}
}
func TestReactiveServesBeforeCookingAndNeverSpeculates(t *testing.T) {
	s := reactiveFixture()
	s.Tray["cake"] = 1
	if PlanReactive(s).Kind != "serve" {
		t.Fatal("serve held food first")
	}
	s.Tray = map[string]int{}
	s.Demands = nil
	if PlanReactive(s).Kind != "wait" {
		t.Fatal("no precooking without demand")
	}
}

func TestKitchenLeavesForReadyFoodBeforeExtendingUncoveredOrders(t *testing.T) {
	s := reactiveFixture()
	s.Phase = "kitchen"
	s.Ready["cake"] = 1
	s.Demands = append(s.Demands, Demand{ID: "seat2-food", Seat: "2", Allowed: []string{"cake"}})
	if d := PlanReactive(s); d.Kind != "pickup" {
		t.Fatalf("collect ready food before extending the kitchen visit: %+v", d)
	}
	s.Kitchens[0].Queued["cake"] = 1
	if d := PlanReactive(s); d.Kind != "pickup" {
		t.Fatalf("all orders covered; leave kitchen without duplicates: %+v", d)
	}
	s.Phase = "active"
	if d := PlanReactive(s); d.Kind != "pickup" {
		t.Fatalf("world mode must collect ready food before more cooking: %+v", d)
	}
}

func TestKitchenLeavesReadyFoodInsteadOfExtendingBusyQueues(t *testing.T) {
	s := reactiveFixture()
	s.Phase = "kitchen"
	s.Ready["cake"] = 1
	s.Demands = append(s.Demands, Demand{ID: "seat2-food", Seat: "2", Allowed: []string{"cake"}})
	for i := range s.Kitchens {
		s.Kitchens[i].Queued["other"] = 1
		s.Kitchens[i].BusySeconds = 10
	}
	if d := PlanReactive(s); d.Kind != "pickup" {
		t.Fatal("ready meal stranded while extending backlogs", d)
	}
	s.Kitchens[1].Queued = map[string]int{}
	s.Kitchens[1].BusySeconds = 0
	if d := PlanReactive(s); d.Kind != "pickup" {
		t.Fatal("new idle capacity must not restart a ready-food kitchen visit", d)
	}
	s.Ready = map[string]int{}
	if d := PlanReactive(s); d.Kind != "queue" || d.Kitchen != "2" {
		t.Fatal("start idle work while no food is waiting for collection", d)
	}
	s.Tray["cake"] = 1
	if d := PlanReactive(s); d.Kind != "serve" {
		t.Fatal("carried meal stranded for further menu work", d)
	}
}

func TestKitchenCollectsDueJobsBeforeExtendingBacklogs(t *testing.T) {
	s := reactiveFixture()
	s.Phase, s.CollectionDue = "kitchen", true
	for i := range s.Kitchens {
		s.Kitchens[i].Queued["other"] = 1
		s.Kitchens[i].BusySeconds = 10
	}
	if d := PlanReactive(s); d.Kind == "queue" {
		t.Fatal("a passed cook ETA should yield to collection", d)
	}
	s.CurrentKitchen, s.CurrentKitchenIdle = "2", true
	if d := PlanReactive(s); d.Kind != "collect" {
		t.Fatal("a newly idle kitchen must not extend an already due collection", d)
	}
	s.CollectionDue = false
	if d := PlanReactive(s); d.Kind != "queue" || d.Kitchen != "2" {
		t.Fatal("idle kitchen should start work before collection becomes due", d)
	}
	s.CurrentKitchenIdle = false
	s.Kitchens[1].Queued = map[string]int{}
	s.Kitchens[1].BusySeconds = 0
	if d := PlanReactive(s); d.Kind != "queue" || d.Kitchen != "2" {
		t.Fatal("still start useful work in the idle kitchen", d)
	}
}

func TestKitchenWaitsBrieflyOnlyForImminentFirstCook(t *testing.T) {
	s := &liveSession{}
	now := time.Unix(1000, 0)
	v := LiveView{ActiveDish: "cake", ActiveSeconds: 2, Tray: map[string]int{}}
	if !s.waitForImminentCook(v, now) || s.waitForImminentCook(v, now.Add(4*time.Second)) {
		t.Fatal("imminent cook must get one bounded fresh-observation window")
	}
	s = &liveSession{}
	v.ActiveSeconds = 12
	if s.waitForImminentCook(v, now) {
		t.Fatal("long cooking should not hold the player in the menu")
	}
	v.ActiveSeconds = 1
	v.Tray["cake"] = 1
	if s.waitForImminentCook(v, now) {
		t.Fatal("carried meals should leave immediately")
	}
}

func TestKitchenSwitchMustImproveCompletionIncludingObservedSwitchCost(t *testing.T) {
	s := reactiveFixture()
	s.Phase, s.CurrentKitchen = "kitchen", "1"
	s.Kitchens[0].CookSeconds["cake"] = 15
	s.Kitchens[1].CookSeconds["cake"] = 14
	if d := PlanReactive(s); d.Kitchen != "1" {
		t.Fatalf("three-second switch must not win for one-second cooking gain: %+v", d)
	}
	s.Kitchens[0].BusySeconds = 20
	if d := PlanReactive(s); d.Kitchen != "2" {
		t.Fatalf("switch when the idle kitchen still finishes substantially sooner: %+v", d)
	}
}

func TestKitchenStartsIdleCookThenAvoidsSwitchingBetweenBusyCooks(t *testing.T) {
	s := reactiveFixture()
	s.Phase, s.CurrentKitchen = "kitchen", "1"
	s.Kitchens[0].BusySeconds = 1
	s.Kitchens[0].Queued["other"] = 1
	s.Kitchens[1].CookSeconds["cake"] = 20
	if d := PlanReactive(s); d.Kitchen != "1" {
		t.Fatal("one second of backlog must not force a slower idle cook", d)
	}
	s.Kitchens[1].BusySeconds = 1
	s.Kitchens[1].Queued["other"] = 1
	s.Kitchens[0].CookSeconds["cake"] = 18
	s.Kitchens[1].CookSeconds["cake"] = 14
	if d := PlanReactive(s); d.Kitchen != "1" {
		t.Fatal("four-second busy-queue advantage cannot justify a select/return cycle", d)
	}
	s.Kitchens[0].BusySeconds = 30
	if d := PlanReactive(s); d.Kitchen != "2" {
		t.Fatal("material completion advantage should still permit switching", d)
	}
}
func TestGourmandIngredientsAndNegation(t *testing.T) {
	d := DishDefinition{Category: "dessert", IngredientsComplete: true, Ingredients: map[string]int{"Amber Rice": 3, "Sugar": 2}}
	r := Requirements{Parsed: true, ExcludeIngredients: []string{"rice grains"}}
	if r.Accepts(d) {
		t.Fatal("no rice must reject rice cake")
	}
	r = Requirements{Parsed: true, ExcludeTypes: []string{"entree"}}
	if !r.Accepts(d) {
		t.Fatal("dessert matches not entree")
	}
	d.IngredientsComplete = false
	r = Requirements{Parsed: true, ExcludeIngredients: []string{"flour"}}
	if r.Accepts(d) {
		t.Fatal("unknown ingredients cannot prove absence")
	}
	r.Parsed = false
	if r.Accepts(d) {
		t.Fatal("unparsed request is not unrestricted")
	}
}
func TestTimingBoundsAndRepeatedFrames(t *testing.T) {
	tracker := &TimingTracker{}
	start := time.Unix(1000, 0)
	obs := TimingObservation{ID: "visit1", Kind: "diner", Phase: "waiting", Seat: "1"}
	tracker.Observe(start, []TimingObservation{obs})
	obs.Phase = "dining"
	tracker.Observe(start.Add(2*time.Second), []TimingObservation{obs})
	tracker.Observe(start.Add(10*time.Second), []TimingObservation{obs})
	obs.Phase = "bill_ready"
	events, e := tracker.Observe(start.Add(12*time.Second), []TimingObservation{obs})
	if e != nil || len(events) != 1 || !events[0].Measured || events[0].SecondsMin != 8 || events[0].SecondsMax != 12 {
		t.Fatalf("wrong observed interval: %+v %v", events, e)
	}
	again, _ := tracker.Observe(start.Add(13*time.Second), []TimingObservation{obs})
	if len(again) != 0 {
		t.Fatal("same frame must not duplicate event")
	}
}
func TestTimingDoesNotInventCompletionFromDisappearance(t *testing.T) {
	tracker := &TimingTracker{}
	start := time.Unix(1000, 0)
	obs := TimingObservation{ID: "k1-job1", Kind: "cook", Kitchen: "1", Chef: "Ember", Dish: "cake", Phase: "cooking"}
	tracker.Observe(start, []TimingObservation{obs})
	events, _ := tracker.Observe(start.Add(10*time.Second), nil)
	if len(events) != 0 {
		t.Fatal("disappeared job is not a completion")
	}
	obs.Phase = "ready"
	events, _ = tracker.Observe(start.Add(15*time.Second), []TimingObservation{obs})
	if len(events) != 1 || events[0].Measured {
		t.Fatal("partially observed initial cooking cannot give exact duration")
	}
}

func TestQueueCompletionRaceRequiresPositiveTimerEvidence(t *testing.T) {
	p := pendingInput{Kind: "queue", Dish: "soup", Kitchen: "1", Before: 1, BeforeSeconds: 2}
	v := LiveView{Kitchen: "1", QueueVerified: true, Queue: map[string]int{"soup": 1}, ActiveDish: "soup", ActiveSeconds: 36}
	if !queuedInputConfirmed(p, v) {
		t.Fatal("same count with a confirmed timer restart is a new cook")
	}
	v.ActiveSeconds = 1
	if queuedInputConfirmed(p, v) {
		t.Fatal("same old job is not confirmation")
	}
	v.ActiveSeconds = 36
	p.BeforeSeconds = 0
	if queuedInputConfirmed(p, v) {
		t.Fatal("unknown old timer cannot prove restart")
	}
	p.BeforeSeconds = 2
	v.QueueVerified = false
	if queuedInputConfirmed(p, v) {
		t.Fatal("ambiguous queue cannot confirm input")
	}
}

func TestPausedIntentRevalidatesControlBeforeSending(t *testing.T) {
	p := pendingInput{Kind: "open", Box: [4]int{900, 640, 100, 23}, At: time.Unix(1, 0)}
	v := LiveView{Phase: "active", AddOrderBox: [4]int{914, 640, 86, 23}}
	if _, ok := eligibleInput(v, p); !ok {
		t.Fatal("an unsent open remains valid after a long pause")
	}
	v.Phase = "ended"
	if _, ok := eligibleInput(v, p); ok {
		t.Fatal("changed screen must cancel old intent")
	}
	p = pendingInput{Kind: "queue", Kitchen: "1", Dish: "cake"}
	v = LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true, RemainingSeconds: 30, Recipes: []RecipeView{{Dish: "cake", Available: true, Seconds: 14}}, Seats: []SeatView{{Dishes: []string{"cake"}}}}
	if _, ok := eligibleInput(v, p); !ok {
		t.Fatal("visible uncovered recipe should remain eligible")
	}
	v.RemainingSeconds = 10
	if _, ok := eligibleInput(v, p); ok {
		t.Fatal("a pause past the cooking deadline must cancel input")
	}
	v.RemainingSeconds = 30
	v.Seats = nil
	if _, ok := eligibleInput(v, p); ok {
		t.Fatal("vanished demand must cancel input")
	}
}

func TestCurrentSoupSpecialistGetsSoupBeforeKitchenSwitch(t *testing.T) {
	s := reactiveFixture()
	s.Phase, s.CurrentKitchen = "kitchen", "2"
	s.Dishes = []DishDefinition{{ID: "cracker", Category: "dessert"}, {ID: "soup", Category: "soup"}}
	s.Demands = []Demand{{ID: "3/0", Seat: "3", Allowed: []string{"cracker"}}, {ID: "1/0", Seat: "1", Allowed: []string{"soup"}}}
	s.Kitchens[0].Specialties = []string{"dessert"}
	s.Kitchens[1].Specialties = []string{"soup"}
	s.Kitchens[0].CookSeconds = map[string]float64{"cracker": 14, "soup": 37}
	s.Kitchens[1].CookSeconds = map[string]float64{"cracker": 15, "soup": 34}
	for i := range s.Kitchens {
		s.Kitchens[i].Available = map[string]bool{"cracker": true, "soup": true}
	}
	d := PlanReactive(s)
	if d.Kind != "queue" || d.Kitchen != "2" || d.Dish != "soup" {
		t.Fatalf("current soup specialist wasted on crackers or switched away: %+v", d)
	}
	s.Kitchens[1].Queued["soup"] = 1
	s.Kitchens[1].BusySeconds = 34
	d = PlanReactive(s)
	if d.Kitchen != "1" || d.Dish != "cracker" {
		t.Fatalf("start dessert cook after soup is reserved: %+v", d)
	}
	s.Tray["soup"] = 1
	if d = PlanReactive(s); d.Kind != "serve" {
		t.Fatalf("held food must bypass cooking allocation: %+v", d)
	}
}
