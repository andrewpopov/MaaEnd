package innkeeping

import "testing"

func TestMeasuredTableFiveTransitPose(t *testing.T) {
	ref := navImage(t, "inn-transit-table5-reference")
	var scene sceneFingerprint
	for i, box := range [][4]int{{570, 170, 120, 40}, {850, 270, 120, 40}} {
		scene[i] = sampleScene(ref, box)
	}
	p := &transitPose{Number: "5", Box: [4]int{798, 320, 44, 60}, Template: "testdata/inn-transit-table5-board.png"}
	if !sceneMatches(scene, scene) {
		t.Fatal("reviewed static window and column anchors lack texture")
	}
	if _, ok := transitPoseGeometry(navImage(t, "inn-transit-table5-replay"), scene, p); !ok {
		t.Fatal("actual measured Table 5 replay must satisfy its numbered physical pose")
	}
	if _, ok := transitPoseGeometry(navImage(t, "inn-table-two-approach"), scene, p); ok {
		t.Fatal("a neighboring table must not satisfy Table 5's physical pose")
	}
}
