package autofight

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
	"time"
)

func comboTestFrame(slots ...int) screenFrame {
	f := screenFrame{Timestamp: time.Now(), Detections: []screenDetection{{Label: LabelCharacterComboActive}}}
	for _, slot := range slots {
		f.Detections = append(f.Detections, screenDetection{Label: LabelCharacterComboFull, Box: adbCharacterRegions[slot-1]})
	}
	return f
}
func TestComboDispatchRejectsExpiredAndWrongOwner(t *testing.T) {
	sa := &ScreenAnalyzer{mobileLayout: true, frames: []screenFrame{comboTestFrame(2)}}
	if !comboDispatchAllowed(sa, 2) {
		t.Fatal("current intended prompt rejected")
	}
	if comboDispatchAllowed(sa, 3) {
		t.Fatal("wrong operator accepted")
	}
	sa.frames = append(sa.frames, screenFrame{Timestamp: time.Now(), Detections: []screenDetection{{Label: LabelCharacterComboActive, Box: maa.Rect{800, 400, 20, 20}}}})
	if comboDispatchAllowed(sa, 2) {
		t.Fatal("old portrait readiness accepted")
	}
	sa.frames = append(sa.frames, screenFrame{Timestamp: time.Now()})
	if comboDispatchAllowed(sa, 0) {
		t.Fatal("expired shared prompt accepted")
	}
}
func TestOrderedComboRejectsAmbiguousOwners(t *testing.T) {
	sa := &ScreenAnalyzer{mobileLayout: true, frames: []screenFrame{comboTestFrame(2, 3)}}
	if comboDispatchAllowed(sa, 2) {
		t.Fatal("ambiguous shared button accepted")
	}
}
