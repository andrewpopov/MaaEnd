package innkeeping

import (
	"testing"
	"time"
)

func TestOrderAgePreservesMissingFramesAndSeparatesVisits(t *testing.T) {
	now := time.Unix(1000, 0)
	s := &liveSession{ID: "round", Visit: [6]int{1}}
	obs := TimingObservation{ID: "round/seat1-visit1", Kind: "diner", Seat: "1", Phase: "waiting"}
	if _, err := s.Tracker.Observe(now, []TimingObservation{obs}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tracker.Observe(now.Add(10*time.Second), nil); err != nil {
		t.Fatal(err)
	}
	if got := s.orderWaitingSeconds("1", now.Add(15*time.Second)); got != 15 {
		t.Fatal("missing frame lost age", got)
	}
	s.Visit[0] = 2
	if got := s.orderWaitingSeconds("1", now.Add(15*time.Second)); got != 0 {
		t.Fatal("new diner inherited age", got)
	}
	obs.ID = "round/seat1-visit2"
	obs.Phase = "ordering"
	if _, err := s.Tracker.Observe(now.Add(20*time.Second), []TimingObservation{obs}); err != nil {
		t.Fatal(err)
	}
	if got := s.orderWaitingSeconds("1", now.Add(25*time.Second)); got != 0 {
		t.Fatal("ordering counted as waiting", got)
	}
	obs.Phase = "waiting"
	if _, err := s.Tracker.Observe(now.Add(30*time.Second), []TimingObservation{obs}); err != nil {
		t.Fatal(err)
	}
	if got := s.orderWaitingSeconds("1", now.Add(35*time.Second)); got != 5 {
		t.Fatal("new order age incorrect", got)
	}
}

func TestObservedAgeReachesCookingDemands(t *testing.T) {
	now := time.Now().Add(-20 * time.Second)
	s := &liveSession{ID: "round", Visit: [6]int{1}}
	_, err := s.Tracker.Observe(now, []TimingObservation{{ID: "round/seat1-visit1", Kind: "diner", Seat: "1", Phase: "waiting"}})
	if err != nil {
		t.Fatal(err)
	}
	state := s.state(LiveView{Seats: []SeatView{{Seat: "1", Phase: "waiting", Dishes: []string{"fin_dumpling"}}}})
	if len(state.Demands) != 1 || state.Demands[0].WaitingSeconds < 20 || state.Demands[0].WaitingSeconds > 22 {
		t.Fatal("cooking demand did not receive observed age", state.Demands)
	}
}
