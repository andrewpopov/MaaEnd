package autofight

import "testing"

func TestInterruptPriorityState(t *testing.T) {
	var s interruptState
	if _, ok := s.next(true, 0, []int{2}, []int{2}, nil); ok {
		t.Fatal("no SP")
	}
	if slot, ok := s.next(true, 1, []int{2}, []int{2}, nil); !ok || slot != 2 {
		t.Fatal("missed configured interrupt")
	}
	if _, ok := s.next(true, 1, []int{2}, []int{2}, nil); ok {
		t.Fatal("duplicate cast")
	}
	s.next(false, 1, nil, nil, nil)
	if _, ok := s.next(true, 1, []int{2}, []int{2}, nil); ok {
		t.Fatal("single missed frame rearmed")
	}
	s.next(false, 1, nil, nil, nil)
	s.next(false, 1, nil, nil, nil)
	if _, ok := s.next(true, 1, []int{2}, []int{2}, nil); !ok {
		t.Fatal("new episode not armed")
	}
}
func TestInterruptRequiresExplicitAvailableSlot(t *testing.T) {
	for _, slots := range [][]int{nil, {0, 5}, {2}} {
		var s interruptState
		if _, ok := s.next(true, 1, slots, []int{1}, nil); ok {
			t.Fatal("used unavailable/unconfigured slot")
		}
	}
}
