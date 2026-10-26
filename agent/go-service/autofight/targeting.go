package autofight

import "time"

const mobileTargetingInterval = 1200 * time.Millisecond

// mobileTargetingState acquires a visible target once, or turns the camera and
// waits for a new frame. Target recovery must never translate the character.
type mobileTargetingState struct {
	lastActionAt         time.Time
	lastTargetAt         time.Time
	lockAttempted        bool
	bossAcquireAttempted bool
	lastLockAttemptAt    time.Time
}

func (sa *ScreenAnalyzer) mobileTargetCentered() bool {
	return sa.hasLabelInFrames(LabelEnemyTarget, 1, false, enemyTargetCenterRegion) ||
		sa.hasLabelInFrames(LabelEnemyLocked, 1, false, enemyTargetCenterRegion)
}

func (s *mobileTargetingState) next(sa *ScreenAnalyzer, now time.Time) (ActionType, bool) {
	if sa.GetEnemyLocked() {
		s.lastTargetAt = now
		if !s.lastActionAt.IsZero() && now.Sub(s.lastActionAt) < mobileTargetingInterval {
			return 0, false
		}
		// Preserve lock, but recenter an offscreen locked marker rather than idle.
		if box, ok := sa.latestLabelBox(LabelEnemyLocked, 1); ok && !sa.mobileTargetCentered() {
			s.lastActionAt = now
			if box[0]+box[2]/2 < 640 {
				return ActionFaceEnemyLeft, true
			}
			return ActionFaceEnemyRight, true
		}
		return 0, false
	}
	if sa.GetEnemyBossHealth() && (!s.bossAcquireAttempted || now.Sub(s.lastLockAttemptAt) >= 4*time.Second) {
		s.bossAcquireAttempted = true
		s.lastLockAttemptAt = now
		s.lastActionAt = now
		return ActionLockTarget, true
	}

	if sa.mobileTargetCentered() {
		s.lastTargetAt = now
		if sa.GetEnemyLocked() || s.lockAttempted {
			return 0, false
		}
		s.lockAttempted = true
		s.lastActionAt = now
		return ActionLockTarget, true
	}
	if !s.lastTargetAt.IsZero() && now.Sub(s.lastTargetAt) < mobileTargetingInterval {
		return 0, false
	}
	if !s.lastActionAt.IsZero() && now.Sub(s.lastActionAt) < mobileTargetingInterval {
		return 0, false
	}
	s.lastActionAt = now
	s.lockAttempted = false
	// A touch drag moves the scene with the finger: drag right to face left.
	if sa.GetEnemyFacingLeft() && !sa.GetEnemyFacingRight() {
		return ActionFaceEnemyLeft, true
	}
	// Right, behind, and unknown bearings use one camera step, then recognize again.
	return ActionFaceEnemyRight, true
}

// A continuous loss of target gets a bounded recovery window. A verified target
// resets the budget; bearings alone do not.
type targetAcquisitionBudget struct{ missingSince time.Time }

func (s *targetAcquisitionBudget) expired(now time.Time, verified bool) bool {
	if verified {
		s.missingSince = time.Time{}
		return false
	}
	if s.missingSince.IsZero() {
		s.missingSince = now
	}
	return now.Sub(s.missingSince) >= 20*time.Second
}
