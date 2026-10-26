package innkeeping

import "testing"

func TestKitchenQueueAcknowledgementRejectsWrongAndUnknownState(t *testing.T) {
	v := LiveView{Phase: "kitchen", Kitchen: "2", QueueVerified: true, Queue: map[string]int{"hot_sour_soup": 2}}
	if !queueCountMatches(v, "2", 2) {
		t.Fatal("two observed soups were not acknowledged")
	}
	if queueCountMatches(v, "1", 2) || queueCountMatches(v, "2", 1) {
		t.Fatal("wrong kitchen or missing queued copy admitted")
	}
	v.Unknown = []string{"unrecognized kitchen queue item"}
	if queueCountMatches(v, "2", 2) {
		t.Fatal("unknown queue admitted")
	}
}

func TestKitchenBatchAcknowledgementRequiresExactRecipes(t *testing.T) {
	v := LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true, Queue: map[string]int{"fin": 1, "cracker": 1}}
	if !queueContentsMatch(v, map[string]int{"fin": 1, "cracker": 1}) {
		t.Fatal("exact queue rejected")
	}
	if queueContentsMatch(v, map[string]int{"fin": 1, "soup": 1}) {
		t.Fatal("same total, wrong dish admitted")
	}
	if queueContentsMatch(v, map[string]int{}) {
		t.Fatal("untracked queue admitted")
	}
	if queueContentsMatch(v, map[string]int{"fin": -1}) {
		t.Fatal("negative stock admitted")
	}
}
