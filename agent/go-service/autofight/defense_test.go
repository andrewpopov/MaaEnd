package autofight

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
	"time"
)

func defenseFrame(sa *ScreenAnalyzer, selected, critical int, healthy ...int) {
	regions := winCharacterRegions
	if sa.mobileLayout {
		regions = adbCharacterRegions
	}
	frame := screenFrame{Timestamp: time.Now()}
	if selected > 0 {
		frame.Detections = append(frame.Detections, screenDetection{Label: LabelCharacterSelect, Box: regions[selected-1]})
	}
	if critical > 0 {
		frame.Detections = append(frame.Detections, screenDetection{Label: LabelCharacterHealthDangerous, Box: regions[critical-1]})
	}
	for _, slot := range healthy {
		frame.Detections = append(frame.Detections, screenDetection{Label: LabelCharacterHealthNormal, Box: regions[slot-1]})
	}
	sa.frames = append(sa.frames, frame)
}

func TestEmergencySwitchWaitsForNewSelectedPortrait(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		sa := &ScreenAnalyzer{mobileLayout: mobile}
		state := emergencySwitchState{}
		defenseFrame(sa, 1, 1, 2, 3)
		if got := state.next(sa, true); got != 2 {
			t.Fatalf("mobile=%v: first switch=%d", mobile, got)
		}
		for i := 0; i < 12; i++ {
			defenseFrame(sa, 1, 1, 2, 3)
			if got := state.next(sa, true); got != 0 {
				t.Fatalf("repeated switch before acknowledgement: %d", got)
			}
		}
		defenseFrame(sa, 0, 1, 2, 3)
		if got := state.next(sa, true); got != 0 || state.pendingTo != 2 {
			t.Fatal("missing portrait cannot confirm a switch")
		}
		defenseFrame(sa, 2, 1, 2, 3)
		if got := state.next(sa, true); got != 0 || state.pendingTo != 0 {
			t.Fatal("fresh operator 2 must acknowledge the switch")
		}
		if got := sa.GetCharacterSelect(); got != 2 {
			t.Fatalf("old operator 1 must not override fresh operator 2, got %d", got)
		}
		defenseFrame(sa, 2, 2, 3)
		if got := state.next(sa, true); got != 3 {
			t.Fatalf("new critical operator must switch to 3, got %d", got)
		}
	}
}

func TestEmergencySwitchRejectsStaleHealthAndConflictingCandidates(t *testing.T) {
	sa := &ScreenAnalyzer{}
	defenseFrame(sa, 1, 1, 2)
	defenseFrame(sa, 1, 1)
	if got := (&emergencySwitchState{}).next(sa, true); got != 0 {
		t.Fatal("historical healthy portrait must not authorize a switch")
	}
	defenseFrame(sa, 1, 1, 2)
	sa.frames[len(sa.frames)-1].Detections = append(sa.frames[len(sa.frames)-1].Detections, screenDetection{Label: LabelCharacterDied, Box: winCharacterRegions[1]})
	if got := (&emergencySwitchState{}).next(sa, true); got != 0 {
		t.Fatal("dead slot must not be selected")
	}
	defenseFrame(sa, 1, 1, 2)
	if got := (&emergencySwitchState{}).next(sa, false); got != 0 {
		t.Fatal("disabled health switching must not issue input")
	}
	sa.frames[len(sa.frames)-1].Detections = append(sa.frames[len(sa.frames)-1].Detections, screenDetection{Label: LabelCharacterSelect, Box: maa.Rect(winCharacterRegions[1])})
	if got := (&emergencySwitchState{}).next(sa, true); got != 0 {
		t.Fatal("ambiguous current selection must not authorize a switch")
	}
}
