package innkeeping

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestKitchenOverlayUsesRetainedInventoryBeforeInspectingOtherKitchen(t *testing.T) {
	v := liveFixture(t, "inn-live-k1")
	if v.Phase != "kitchen" || trayCount(v.Tray) != 0 {
		t.Fatal("fixture must exercise the kitchen overlay's hidden tray")
	}
	s := &liveSession{Started: true, Tray: map[string]int{"fin_dumpling": 1}, Kitchens: map[string]ChefKitchen{}}
	state := s.state(v)
	if !kitchenExitRequired(state, PlanReactive(state)) {
		t.Fatal("hidden carried meal lost to an unverified-kitchen tab tour")
	}
}

func TestKitchenPendingInputYieldsToFreshCompletion(t *testing.T) {
	at := time.Unix(1000, 0)
	s := &liveSession{
		Started: true, Kitchens: map[string]ChefKitchen{}, Tray: map[string]int{},
		Jobs: []cookJob{{Kitchen: "1", Dish: "fin_dumpling", ExpectedAt: at.Add(30 * time.Second)}},
	}
	v := LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true, Queue: map[string]int{}, Seats: []SeatView{{Seat: "1", Dishes: []string{"fin_dumpling"}}}}
	if !s.kitchenInputShouldExit(v, at, at.Add(time.Second)) || !s.Jobs[0].CounterCheckDue {
		t.Fatal("stale menu input ignored a dish absent from its freshly verified queue")
	}
	s = &liveSession{
		Started: true, Kitchens: map[string]ChefKitchen{}, Tray: map[string]int{},
		Jobs: []cookJob{{Kitchen: "1", Dish: "fin_dumpling", CookSeconds: 18, QueuedAt: at.Add(-10 * time.Second), ExpectedAt: at.Add(time.Second)}},
	}
	v.Queue, v.ActiveDish, v.ActiveSeconds = map[string]int{"fin_dumpling": 1}, "fin_dumpling", 1
	if !s.kitchenInputShouldExit(v, at, at.Add(2*time.Second)) {
		t.Fatal("a cook becoming due during recognition did not cancel the stale input")
	}
}

func TestArchivedKitchenBatchHonorsUsefulTailBeforeCollection(t *testing.T) {
	var trace struct {
		MenuOpenAt   time.Time `json:"menu_open_at"`
		MenuCloseAt  time.Time `json:"menu_close_at"`
		Observations []struct {
			At          time.Time `json:"at"`
			Kind        string    `json:"kind"`
			Dish        string    `json:"dish"`
			ConfirmedAt time.Time `json:"confirmed_at"`
			View        LiveView  `json:"view"`
		} `json:"observations"`
	}
	data, err := os.ReadFile("testdata/inn-kitchen-trace-20261019.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &liveSession{Started: true, Catalog: catalog, Tray: map[string]int{}, Kitchens: map[string]ChefKitchen{}}
	var firstDue time.Time
	preventedQueues := 0
	batchHolds := 0
	for i, observation := range trace.Observations {
		v := observation.View
		if observation.ConfirmedAt.Before(observation.At) {
			t.Fatal("archive is not a confirmed chronological input trace", i)
		}
		if v.Phase != "kitchen" {
			s.Tray = v.Tray
		} else {
			k := ChefKitchen{ID: v.Kitchen, Verified: v.QueueVerified, Capacity: 3, CookSeconds: map[string]float64{}, Available: map[string]bool{}}
			for _, recipe := range v.Recipes {
				k.CookSeconds[recipe.Dish] = recipe.Seconds
				k.Available[recipe.Dish] = recipe.Available
			}
			s.Kitchens[v.Kitchen] = k
			reconcileCookCountdown(s.Jobs, v, k, observation.At)
			state := s.stateAt(v, observation.At)
			decision := PlanReactive(state)
			if state.CollectionDue && state.HoldKitchen {
				batchHolds++
				if kitchenExitRequired(state, decision) || decision.Kind == "pickup" || decision.Kind == "collect" {
					t.Fatal("first finished portion overrode useful batch tail", observation.At, decision)
				}
			}
			if state.CollectionDue && !state.HoldKitchen && observation.At.After(trace.MenuOpenAt) && observation.At.Before(trace.MenuCloseAt) {
				if firstDue.IsZero() {
					firstDue = observation.At
				}
				if !kitchenExitRequired(state, decision) || decision.Kind == "queue" {
					t.Fatal("collectible meal was stranded by another kitchen dispatch", observation.At, decision)
				}
				if observation.Kind == "queue" {
					preventedQueues++
				}
			}
		}
		// Continue shadowing the historical actions, rather than simulating
		// changed game outcomes. This verifies decisions against real views.
		switch observation.Kind {
		case "queue":
			p := pendingInput{Kitchen: v.Kitchen, Dish: observation.Dish, IssuedAt: observation.At}
			for _, recipe := range v.Recipes {
				if recipe.Dish == p.Dish {
					p.CookSeconds = recipe.Seconds
				}
			}
			job := estimatedCook(s.Jobs, p, s.Kitchens[v.Kitchen], trayCount(v.Queue) == 0, observation.ConfirmedAt)
			job.ID, job.Phase = fmt.Sprintf("recorded-%d", i), "queued"
			s.Jobs = append(s.Jobs, job)
		case "pickup":
			s.Jobs = retirePickedJob(s.Jobs, observation.Dish, observation.ConfirmedAt)
		}
	}
	if batchHolds < 1 {
		t.Fatal("archive no longer exercises batch holding and due departure", firstDue, preventedQueues, batchHolds)
	}
	t.Logf("%d archived first-copy completions now preserve their useful batch; %d due dispatches yield to collection", batchHolds, preventedQueues)
}
