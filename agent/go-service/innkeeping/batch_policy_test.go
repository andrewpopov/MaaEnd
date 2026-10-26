package innkeeping

import (
	"testing"
	"time"
)

func TestBatchWaitsForUsefulTailNotFirstDishOrOtherKitchenTail(t *testing.T) {
	at := time.Unix(1000, 0)
	v := LiveView{Phase: "kitchen", Seats: []SeatView{{Seat: "1", Dishes: []string{"cracker", "fin"}}, {Seat: "5", Dishes: []string{"fries", "soup"}}}}
	jobs := []cookJob{{Dish: "cracker", Kitchen: "1", ExpectedAt: at.Add(14 * time.Second)}, {Dish: "fin", Kitchen: "1", ExpectedAt: at.Add(34 * time.Second)}, {Dish: "fries", Kitchen: "2", ExpectedAt: at.Add(26 * time.Second)}, {Dish: "soup", Kitchen: "2", ExpectedAt: at.Add(60 * time.Second)}}
	if !holdForBatch(jobs, v, at.Add(15*time.Second)) {
		t.Fatal("first dish incorrectly ended the useful batch")
	}
	if holdForBatch(jobs, v, at.Add(31*time.Second)) {
		t.Fatal("earliest useful kitchen batch missed departure allowance")
	}
	jobs[1].ExpectedAt = time.Time{}
	if holdForBatch(jobs, v, at) {
		t.Fatal("unknown preparation acquired a departure deadline")
	}
}
func TestBatchCreditSurvivesFirstPickupAndResetsAfterLast(t *testing.T) {
	s := &liveSession{Jobs: []cookJob{{Kitchen: "1"}, {Kitchen: "1"}}}
	s.reconcileBatchCounts()
	s.Jobs = s.Jobs[:1]
	s.reconcileBatchCounts()
	if s.BatchIssued["1"] != 2 {
		t.Fatal("first pickup reopened a batch slot")
	}
	s.Jobs = nil
	s.reconcileBatchCounts()
	if s.BatchIssued["1"] != 0 {
		t.Fatal("finished batch blocked next batch")
	}
}
func TestCompletionPriorityIgnoresAgeAndPreservesCopies(t *testing.T) {
	s := reactiveFixture()
	s.Tray["cake"] = 1
	s.Demands = []Demand{{ID: "old-cake", Seat: "1", Allowed: []string{"cake"}, WaitingSeconds: 100}, {ID: "old-soup", Seat: "1", Allowed: []string{"soup"}, WaitingSeconds: 100}, {ID: "new-cake", Seat: "2", Allowed: []string{"cake"}}}
	if d := PlanReactive(s); d.Seat != "2" || d.Kind != "serve" {
		t.Fatal("partial older meal defeated completable newer meal", d)
	}
	s.Demands[0].WaitingSeconds = 0
	s.Demands[2].WaitingSeconds = 1000
	if d := PlanReactive(s); d.Seat != "2" {
		t.Fatal("age changed meal completion priority", d)
	}
}
func TestHoldPropagatesThroughPlannerAndKitchenExecutor(t *testing.T) {
	s := reactiveFixture()
	s.Phase = "kitchen"
	s.CollectionDue = true
	s.HoldKitchen = true
	s.Ready["cake"] = 1
	d := PlanReactive(s)
	if d.Kind != "wait" || kitchenExitRequired(s, d) {
		t.Fatal("ready first dish bypassed batch hold", d)
	}
	s.Tray["cake"] = 1
	if !kitchenExitRequired(s, PlanReactive(s)) {
		t.Fatal("batch hold stranded carried food")
	}
}
func TestTwoIssuedJobsPreventThirdAndAllowOtherKitchen(t *testing.T) {
	s := reactiveFixture()
	s.Kitchens[0].BatchIssued = 2
	if d := PlanReactive(s); d.Kind != "queue" || d.Kitchen != "2" {
		t.Fatal("batch target not enforced", d)
	}
	s.Kitchens[1].BatchIssued = 2
	if d := PlanReactive(s); d.Kind != "wait" {
		t.Fatal("third job inserted", d)
	}
}

func TestFreshFinishedQueueOverridesStalePrediction(t *testing.T) {
	at := time.Unix(1000, 0)
	v := LiveView{Phase: "kitchen", Seats: []SeatView{{Seat: "1", Dishes: []string{"soup"}}}}
	jobs := []cookJob{{Kitchen: "1", Dish: "soup", ExpectedAt: at.Add(time.Hour), CounterCheckDue: true}}
	if holdForBatch(jobs, v, at) {
		t.Fatal("freshly finished queue inherited stale future deadline")
	}
}

func TestDueCounterPickupPreventsKitchenReopenLoop(t *testing.T) {
	state := reactiveFixture()
	state.Phase = "active"
	decision := ReactiveDecision{Kind: "queue"}
	if !kitchenOpenRequired(state, decision, true) {
		t.Fatal("idle counter should admit useful cooking")
	}
	state.CollectionDue = true
	for _, verified := range []bool{false, true} {
		if kitchenOpenRequired(state, decision, verified) {
			t.Fatal("due pickup reopened kitchen before physical collection")
		}
	}
	state.CollectionDue = false
	state.Tray["cake"] = 1
	if kitchenOpenRequired(state, decision, true) {
		t.Fatal("carried food was stranded in another kitchen visit")
	}
}
