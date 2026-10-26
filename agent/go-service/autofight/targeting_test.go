package autofight

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
	"time"
)

func TestMobileTargetRecoveryDoesNotWalkIntoWalls(t *testing.T) {
	now := time.Now()
	sa := &ScreenAnalyzer{mobileLayout: true, frames: []screenFrame{{Timestamp: now}}}
	state := &mobileTargetingState{}
	if action, ok := state.next(sa, now); !ok || action != ActionLockTarget {
		t.Fatal("unknown bearing must first try native lock without walking")
	}
	if sa.mobileTargetCentered() {
		t.Fatal("no target must release held attacks")
	}
	if _, ok := state.next(sa, now.Add(100*time.Millisecond)); ok {
		t.Fatal("must wait for next camera observation")
	}
	if action, ok := state.next(sa, now.Add(1500*time.Millisecond)); !ok || action != ActionFaceEnemyRight {
		t.Fatal("unconfirmed native lock must fall back to camera recovery")
	}
	sa.frames[0].Detections = []screenDetection{{Label: LabelEnemyFacing, Box: maa.Rect{350, 250, 15, 15}}}
	if action, ok := state.next(sa, now.Add(3*time.Second)); !ok || action != ActionFaceEnemyLeft {
		t.Fatal("left bearing must turn camera left without strafing")
	}
}
func TestMobileLockDoesNotToggleRepeatedly(t *testing.T) {
	now := time.Now()
	sa := &ScreenAnalyzer{mobileLayout: true, frames: []screenFrame{{Timestamp: now, Detections: []screenDetection{{Label: LabelEnemyTarget, Box: maa.Rect{600, 250, 20, 20}}}}}}
	state := &mobileTargetingState{}
	if !sa.mobileTargetCentered() {
		t.Fatal("fresh target must allow attack")
	}
	if action, ok := state.next(sa, now); !ok || action != ActionLockTarget {
		t.Fatal("centered target must acquire lock")
	}
	if _, ok := state.next(sa, now.Add(5*time.Second)); ok {
		t.Fatal("must not toggle lock off while target remains visible")
	}
	sa.frames = append(sa.frames, screenFrame{Timestamp: now.Add(6 * time.Second)})
	if sa.mobileTargetCentered() {
		t.Fatal("historical target must not keep lunging at obstacles")
	}
	if action, ok := state.next(sa, now.Add(7*time.Second)); !ok || action != ActionFaceEnemyRight {
		t.Fatal("lost target must restart camera scan")
	}
}
func TestMobileOffscreenLockDoesNotEnableForwardAttacks(t *testing.T) {
	now := time.Now()
	sa := &ScreenAnalyzer{mobileLayout: true, frames: []screenFrame{{Timestamp: now, Detections: []screenDetection{{Label: LabelEnemyLocked, Box: maa.Rect{1100, 250, 20, 20}}}}}}
	if sa.mobileTargetCentered() {
		t.Fatal("offscreen lock must not enable held auto-lunges")
	}
	if _, ok := (&mobileTargetingState{}).next(sa, now); ok {
		t.Fatal("native lock must retain control of the camera")
	}
}

func TestMobileTargetFlickerDoesNotStartCameraSweep(t *testing.T) {
	now := time.Now()
	sa := &ScreenAnalyzer{mobileLayout: true, frames: []screenFrame{{Timestamp: now, Detections: []screenDetection{{Label: LabelEnemyTarget, Box: maa.Rect{600, 250, 20, 20}}}}}}
	state := mobileTargetingState{}
	state.next(sa, now)
	state.next(sa, now.Add(5*time.Second))
	sa.frames = append(sa.frames, screenFrame{Timestamp: now.Add(5100 * time.Millisecond)})
	if _, ok := state.next(sa, now.Add(5100*time.Millisecond)); ok {
		t.Fatal("one missed frame must not move the camera")
	}
	if _, ok := state.next(sa, now.Add(7*time.Second)); !ok {
		t.Fatal("sustained target loss must still allow recovery")
	}
}

func TestBossAcquisitionTriesNativeLockBeforeCamera(t *testing.T) {
	now := time.Now()
	sa := &ScreenAnalyzer{mobileLayout: true, frames: []screenFrame{{Timestamp: now, Detections: []screenDetection{{Label: LabelEnemyBossHealth, Box: maa.Rect{430, 34, 421, 22}}}}}}
	state := mobileTargetingState{}
	if a, ok := state.next(sa, now); !ok || a != ActionLockTarget {
		t.Fatal("boss acquisition must try native lock")
	}
	if _, ok := state.next(sa, now.Add(100*time.Millisecond)); ok {
		t.Fatal("must observe after lock input")
	}
	sa.frames = append(sa.frames, screenFrame{Timestamp: now.Add(time.Second), Detections: []screenDetection{{Label: LabelEnemyLocked, Box: maa.Rect{1100, 250, 20, 20}}}})
	if _, ok := state.next(sa, now.Add(time.Second)); ok {
		t.Fatal("confirmed lock must suppress camera recovery")
	}
	if sa.mobileTargetCentered() {
		t.Fatal("off-center lock must not authorize melee")
	}
}
