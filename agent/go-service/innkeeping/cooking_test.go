package innkeeping

import (
	"testing"
	"time"
)

func TestCookEstimateStartsAtIssuedInputAndChainsBehindKnownWork(t *testing.T) {
	start := time.Unix(1000, 0)
	p := pendingInput{Kitchen: "1", Dish: "soup", IssuedAt: start, CookSeconds: 11}
	k := ChefKitchen{CookSeconds: map[string]float64{"soup": 20}}
	j := estimatedCook(nil, p, k, true, start.Add(3*time.Second))
	if j.ExpectedAt != start.Add(11*time.Second) || j.StartEstimate != start || j.QueuedAt != start || j.CookSeconds != 11 {
		t.Fatal("recognition delay or cached recipe inflated the actual input estimate", j)
	}
	j.Kitchen = "1"
	p.IssuedAt = start.Add(4 * time.Second)
	next := estimatedCook([]cookJob{j}, p, k, false, start.Add(7*time.Second))
	if next.StartEstimate != j.ExpectedAt || next.ExpectedAt != start.Add(22*time.Second) {
		t.Fatal("waiting job did not follow its own kitchen's FIFO timeline", next)
	}
	unknown := cookJob{Kitchen: "1", Dish: "unknown"}
	if got := estimatedCook([]cookJob{unknown}, p, k, false, start); !got.ExpectedAt.IsZero() {
		t.Fatal("unknown earlier work acquired an invented completion time", got)
	}
	j.ExpectedAt = start.Add(100 * time.Second)
	if got := estimatedCook([]cookJob{j}, p, k, true, start); got.StartEstimate != p.IssuedAt {
		t.Fatal("fresh single-active queue inherited an obsolete backlog", got)
	}
}

func TestNextCompletionUsesBothKitchensWithoutReopeningMenus(t *testing.T) {
	at := time.Unix(1000, 0)
	v := LiveView{ActiveDish: "fin", ActiveSeconds: 12, Seats: []SeatView{{Seat: "1", Dishes: []string{"fin", "soup"}}}}
	s := &liveSession{Jobs: []cookJob{
		{ID: "fin", Kitchen: "1", Dish: "fin", ExpectedAt: at.Add(12 * time.Second)},
		{ID: "soup", Kitchen: "2", Dish: "soup", ExpectedAt: at.Add(time.Second)},
		{Kitchen: "2", Dish: "unrequested", ExpectedAt: at},
	}}
	if next := nextCookCompletion(s.Jobs, v); next == nil || next.ID != "soup" {
		t.Fatal("first completion was not compared across both kitchens", next)
	}
	if !s.waitForImminentCook(v, at) {
		t.Fatal("another kitchen's imminent completion unnecessarily required a menu switch")
	}
	s.Jobs[1].CounterCheckDue = true
	if next := nextCookCompletion(s.Jobs, v); next == nil || next.ID != "fin" {
		t.Fatal("counter inspection was confused with pending cooking", next)
	}
}

func TestCookCountdownCorrectsActiveAndFollowingQueueAtCaptureTime(t *testing.T) {
	at := time.Unix(1000, 0)
	jobs := []cookJob{
		{Kitchen: "1", Dish: "fin", QueuedAt: at.Add(-12 * time.Second), CookSeconds: 18, ExpectedAt: at.Add(10 * time.Second)},
		{Kitchen: "2", Dish: "other", QueuedAt: at, ExpectedAt: at.Add(time.Minute)},
		{Kitchen: "1", Dish: "soup", QueuedAt: at.Add(-time.Second), CookSeconds: 11, ExpectedAt: at.Add(30 * time.Second)},
	}
	v := LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true, ActiveDish: "fin", ActiveSeconds: 3, Queue: map[string]int{"fin": 1, "soup": 1}}
	reconcileCookCountdown(jobs, v, ChefKitchen{}, at)
	if jobs[0].ExpectedAt != at.Add(3*time.Second) || jobs[2].StartEstimate != jobs[0].ExpectedAt || jobs[2].ExpectedAt != at.Add(14*time.Second) {
		t.Fatal("displayed countdown did not repair this queue's accumulated drift", jobs)
	}
	if jobs[1].ExpectedAt != at.Add(time.Minute) {
		t.Fatal("one kitchen changed the other kitchen's prediction")
	}
	v.ActiveSeconds = 311
	reconcileCookCountdown(jobs, v, ChefKitchen{}, at)
	if jobs[0].ExpectedAt != at.Add(3*time.Second) {
		t.Fatal("merged OCR corrupted a known cooking estimate")
	}
}

func TestCookCountdownDoesNotReassignOlderSameDishCounterCopy(t *testing.T) {
	at := time.Unix(1000, 0)
	old := cookJob{Kitchen: "1", Dish: "fin", QueuedAt: at.Add(-30 * time.Second), CookSeconds: 18, ExpectedAt: at.Add(-10 * time.Second)}
	jobs := []cookJob{old, {Kitchen: "1", Dish: "fin", QueuedAt: at.Add(-5 * time.Second), CookSeconds: 18}}
	v := LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true, ActiveDish: "fin", ActiveSeconds: 12, Queue: map[string]int{"fin": 1}}
	reconcileCookCountdown(jobs, v, ChefKitchen{}, at)
	old.CounterCheckDue = true
	if jobs[0] != old || jobs[1].ExpectedAt != at.Add(12*time.Second) {
		t.Fatal("counter copy was mistaken for its later cooking copy", jobs)
	}
}

func TestVerifiedCookingQueueSchedulesAbsentCopiesForCounterInspection(t *testing.T) {
	at := time.Now()
	for _, priorFinish := range []time.Time{at.Add(30 * time.Second), {}} {
		jobs := []cookJob{
			{Kitchen: "1", Dish: "fin", ExpectedAt: priorFinish},
			{Kitchen: "1", Dish: "fin", QueuedAt: at.Add(-6 * time.Second), CookSeconds: 18},
		}
		v := LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true, ActiveDish: "fin", ActiveSeconds: 12, Queue: map[string]int{"fin": 1}, Seats: []SeatView{{Seat: "1", Dishes: []string{"fin", "fin"}}}}
		reconcileCookCountdown(jobs, v, ChefKitchen{}, at)
		if !jobs[0].CounterCheckDue || jobs[1].CounterCheckDue {
			t.Fatal("verified queue did not distinguish counter candidate from active copy", jobs)
		}
		s := &liveSession{Started: true, Jobs: jobs, Kitchens: map[string]ChefKitchen{"1": {Verified: true}}}
		state := s.state(v)
		if state.CounterPending["fin"] != 1 || state.Ready["fin"] != 0 || state.Kitchens[0].Queued["fin"] != 1 || !state.CollectionDue {
			t.Fatal("counter candidate became actual stock or occupied a cooking slot", state)
		}
		if !collectibleDishes(jobs, v.Seats, at)["fin"] {
			t.Fatal("stale or absent ETA prevented counter inspection")
		}
		// A confirmed-absent counter candidate outranks a merely expired
		// estimate on the newer active copy of that same dish.
		jobs[1].ExpectedAt = at.Add(-time.Second)
		remaining := retirePickedJob(append([]cookJob(nil), jobs...), "fin", at)
		if len(remaining) != 1 || remaining[0].CounterCheckDue {
			t.Fatal("confirmed pickup retired the active copy instead of counter candidate", remaining)
		}
	}
	jobs := []cookJob{{Kitchen: "1", Dish: "soup"}, {Kitchen: "2", Dish: "other"}}
	v := LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true, Queue: map[string]int{}}
	reconcileCookCountdown(jobs, v, ChefKitchen{}, at)
	if !jobs[0].CounterCheckDue || jobs[1].CounterCheckDue || jobs[0].Phase == "ready" {
		t.Fatal("empty verified queue failed to schedule only its own counter inspection", jobs)
	}
}

func TestAdoptedQueueOnlyPredictsUnambiguousActiveCopy(t *testing.T) {
	at := time.Unix(1000, 0)
	jobs := []cookJob{{Kitchen: "1", Dish: "soup"}, {Kitchen: "1", Dish: "fin"}}
	v := LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true, ActiveDish: "fin", ActiveSeconds: 4, Queue: map[string]int{"fin": 1, "soup": 1}}
	k := ChefKitchen{CookSeconds: map[string]float64{"fin": 18, "soup": 11}}
	reconcileCookCountdown(jobs, v, k, at)
	if jobs[1].ExpectedAt != at.Add(4*time.Second) || !jobs[0].ExpectedAt.IsZero() {
		t.Fatal("adopted active countdown lost or unknown waiting order invented", jobs)
	}
	jobs = []cookJob{{Kitchen: "1", Dish: "fin"}, {Kitchen: "1", Dish: "fin"}}
	v.Queue = map[string]int{"fin": 2}
	reconcileCookCountdown(jobs, v, k, at)
	if !jobs[0].ExpectedAt.IsZero() || !jobs[1].ExpectedAt.IsZero() {
		t.Fatal("ambiguous adopted duplicate received a job identity", jobs)
	}
}

func TestLateCounterVisitDoesNotExtendObservedCooking(t *testing.T) {
	now := time.Unix(1000, 0)
	tracker := TimingTracker{}
	obs := TimingObservation{ID: "soup", Kind: "cook", Kitchen: "2", Dish: "soup", Phase: "queued"}
	tracker.Observe(now, []TimingObservation{obs})
	obs.Phase = "cooking"
	tracker.Observe(now.Add(time.Second), []TimingObservation{obs})
	job := cookJob{Dish: "soup", Kitchen: "2", Phase: "cooking"}
	world := LiveView{Phase: "active"}
	if directlyObservedCook(job, world) {
		t.Fatal("off-screen retained cooking became direct evidence")
	}
	tracker.Observe(now.Add(69*time.Second), nil)
	job.Phase = "ready"
	world.Prompt = "pickup"
	world.PromptDish = "soup"
	if !directlyObservedCook(job, world) {
		t.Fatal("native pickup did not confirm readiness")
	}
	obs.Phase = "ready"
	events, err := tracker.Observe(now.Add(70*time.Second), []TimingObservation{obs})
	if err != nil || len(events) != 1 || events[0].SecondsMin > 1 || events[0].SecondsMax < 69 {
		t.Fatal("late pickup fabricated a narrow long-cook measurement", events, err)
	}
	kitchen := LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true, Queue: map[string]int{"soup": 1}, ActiveDish: "soup", ActiveSeconds: 5}
	job.Phase = "cooking"
	if directlyObservedCook(job, kitchen) {
		t.Fatal("other kitchen updated this cook's timing")
	}
	kitchen.Kitchen = "2"
	if !directlyObservedCook(job, kitchen) {
		t.Fatal("visible active countdown rejected")
	}
	job.CounterCheckDue = true
	if directlyObservedCook(job, kitchen) {
		t.Fatal("older counter copy borrowed a newer countdown")
	}
}
