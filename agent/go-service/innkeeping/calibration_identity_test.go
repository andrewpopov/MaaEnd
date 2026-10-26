package innkeeping

import "testing"

func TestCalibrationIdentityTracksGeometryNotCounters(t *testing.T) {
	original := fixedRoute{Version: 1, ID: "example", Goal: "transit", FromWaypoint: "counter", ToWaypoint: "table1",
		Steps: []movementCommand{{Kind: "walk", X: 195, Y: 471, Milliseconds: 700}}}
	recorded := original
	recorded.Replays = 10
	recorded.InnReplays = 10
	recorded.EmptyReplays = 5
	recorded.LoadedReplays = 5
	if calibrationRouteHash(original) != calibrationRouteHash(recorded) {
		t.Fatal("Replay credit changed route identity")
	}
	changed := original
	changed.Steps = []movementCommand{{Kind: "walk", X: 195, Y: 471, Milliseconds: 800}}
	if calibrationRouteHash(original) == calibrationRouteHash(changed) {
		t.Fatal("Different inputs pooled as one route")
	}
	changed = original
	changed.StartSeatMarker = [4]int{300, 300, 30, 40}
	if calibrationRouteHash(original) == calibrationRouteHash(changed) {
		t.Fatal("Different endpoint guard pooled as one route")
	}
}
