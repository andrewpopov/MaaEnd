package autofight

import (
	"testing"
	"time"
)

func TestTargetAcquisitionBudget(t *testing.T) {
	var b targetAcquisitionBudget
	now := time.Unix(100, 0)
	if b.expired(now, false) || b.expired(now.Add(19*time.Second), false) {
		t.Fatal("premature exhaustion")
	}
	if !b.expired(now.Add(20*time.Second), false) {
		t.Fatal("unbounded search")
	}
	if b.expired(now.Add(21*time.Second), true) {
		t.Fatal("verified target did not reset")
	}
	if b.expired(now.Add(40*time.Second), false) {
		t.Fatal("new loss inherited old budget")
	}
}
