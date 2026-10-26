package autofight

// A persistent telegraph produces one dodge. A newly appearing ground warning
// still takes priority while an enemy warning remains visible.
type dodgeState struct{ active uint8 }

func (s *dodgeState) next(ground, enemy bool) bool {
	var cues uint8
	if ground {
		cues |= 1
	}
	if enemy {
		cues |= 2
	}
	fresh := cues &^ s.active
	s.active = cues
	return fresh != 0
}

// A switch remains pending until a fresh selected-portrait observation changes.
// Historical health and portrait smoothing must not resubmit the same input.
type emergencySwitchState struct{ pendingFrom, pendingTo int }

func (s *emergencySwitchState) next(sa *ScreenAnalyzer, enabled bool) int {
	if len(sa.frames) == 0 {
		return 0
	}
	selected := sa.selectedInFrame(sa.frames[len(sa.frames)-1])
	if selected == 0 {
		return 0
	}
	if s.pendingTo != 0 {
		if selected == s.pendingFrom {
			return 0
		}
		s.pendingFrom, s.pendingTo = 0, 0
	}
	if !enabled {
		return 0
	}
	regions := winCharacterRegions
	if sa.mobileLayout {
		regions = adbCharacterRegions
	}
	if !sa.hasLabelInFrames(LabelCharacterHealthDangerous, 1, false, regions[selected-1]) {
		return 0
	}
	for idx, region := range regions {
		if idx+1 == selected || !sa.hasLabelInFrames(LabelCharacterHealthNormal, 1, false, region) ||
			sa.hasLabelInFrames(LabelCharacterHealthDangerous, 1, false, region) ||
			sa.hasLabelInFrames(LabelCharacterDied, 1, false, region) {
			continue
		}
		s.pendingFrom, s.pendingTo = selected, idx+1
		return s.pendingTo
	}
	return 0
}
