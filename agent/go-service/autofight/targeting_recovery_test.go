package autofight

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
	"time"
)

func TestBossLockRetriesAfterSearch(t *testing.T) {
	now := time.Now()
	sa := &ScreenAnalyzer{mobileLayout: true, frames: []screenFrame{{Timestamp: now, Detections: []screenDetection{{Label: LabelEnemyBossHealth, Box: maa.Rect{430, 34, 421, 22}}}}}}
	var s mobileTargetingState
	s.next(sa, now)
	s.next(sa, now.Add(2*time.Second))
	if a, ok := s.next(sa, now.Add(4*time.Second)); !ok || a != ActionLockTarget {
		t.Fatal("scan must not suppress bounded lock retries")
	}
}
