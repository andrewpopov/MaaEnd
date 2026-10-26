package autofight

import "time"

// Input acceptance is separate from warning resolution: neither proves evasion.
// Each visible warning gets at most one follow-up on a fresh observation.
const dodgeRetryInterval = 750 * time.Millisecond
const dodgeInputInterval = 300 * time.Millisecond

type dodgeWarning struct {
	detection          screenDetection
	attempts           int
	lastAttempt        time.Time
	controllerAccepted bool
}

type dodgeState struct {
	warnings  []*dodgeWarning
	lastInput time.Time
}

func (s *dodgeState) observe(detections []screenDetection, now time.Time) *dodgeWarning {
	previous := s.warnings
	used := make([]bool, len(previous))
	s.warnings = nil
	for _, det := range detections {
		var warning *dodgeWarning
		for i, old := range previous {
			if !used[i] && old.detection.Label == det.Label && boxIntersects(old.detection.Box, det.Box) {
				warning, used[i] = old, true
				break
			}
		}
		if warning == nil {
			warning = &dodgeWarning{}
		}
		warning.detection = det
		s.warnings = append(s.warnings, warning)
	}
	if !s.lastInput.IsZero() && now.Sub(s.lastInput) < dodgeInputInterval {
		return nil
	}
	// New threats take priority over an unconfirmed previous input.
	for _, warning := range s.warnings {
		if warning.attempts == 0 {
			return warning
		}
	}
	for _, warning := range s.warnings {
		if warning.attempts < 2 && now.Sub(warning.lastAttempt) >= dodgeRetryInterval {
			return warning
		}
	}
	return nil
}

func (s *dodgeState) dispatched(warning *dodgeWarning, now time.Time, accepted bool) {
	warning.attempts++
	warning.lastAttempt, warning.controllerAccepted = now, accepted
	s.lastInput = now
}

func (sa *ScreenAnalyzer) dodgeWarnings(compat bool) []screenDetection {
	var result []screenDetection
	if len(sa.frames) == 0 {
		return result
	}
	for _, det := range sa.frames[len(sa.frames)-1].Detections {
		switch det.Label {
		case LabelEnemyDodge:
			if compat && pointInRect(det.Box[0]+det.Box[2]/2, det.Box[1]+det.Box[3]/2, dodgeCompatRegion) {
				continue
			}
		case LabelEnemyAttackGroundDodge:
		default:
			continue
		}
		result = append(result, det)
	}
	return result
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
