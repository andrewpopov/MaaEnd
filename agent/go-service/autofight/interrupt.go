package autofight

import "slices"

// One attempt per visible telegraph; two absent observations rearm the response.
// Slots are supplied explicitly: visual charge detection does not establish
// which operator's skill can interrupt this enemy.
type interruptState struct {
	attempted bool
	absent    int
}

func (s *interruptState) next(cue bool, energy int, slots, healthy, dangerous []int) (int, bool) {
	if !cue {
		s.absent++
		if s.absent >= 2 {
			s.attempted = false
		}
		return 0, false
	}
	s.absent = 0
	if s.attempted || energy < 1 {
		return 0, false
	}
	for _, slot := range slots {
		if slot < 1 || slot > 4 || !slices.Contains(healthy, slot) || slices.Contains(dangerous, slot) {
			continue
		}
		s.attempted = true
		return slot, true
	}
	return 0, false
}
