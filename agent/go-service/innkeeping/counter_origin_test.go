package innkeeping

import (
	"image"
	"os"
	"testing"
)

func TestCounterCorrectionRejectsConflictingAndOutOfPlaneLandmarks(t *testing.T) {
	for _, offsets := range [][2][2]int{{{12, 0}, {-12, 0}}, {{12, 0}, {28, 0}}, {{12, 14}, {12, 0}}, {{80, 0}, {80, 0}}, {{0, 0}, {0, 0}}} {
		if mode := counterCorrectionMode(offsets); mode != "" {
			t.Fatalf("unsafe correction %v: %s", offsets, mode)
		}
	}
}

func TestCounterNearbyMatchDoesNotAuthorizeDeparture(t *testing.T) {
	f, err := os.Open("testdata/inn-route-counter-reference.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ref, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	scene := fingerprint(ref)
	shifted := image.NewRGBA(ref.Bounds())
	for y := 0; y < 720; y++ {
		for x := 0; x < 1268; x++ {
			shifted.Set(x+12, y, ref.At(x, y))
		}
	}
	if _, ok := counterOriginMatch(shifted, counterOriginParams{Reference: scene, Mode: "aligned"}); ok {
		t.Fatal("nearby pose authorized departure")
	}
	if offsets, ok := counterOriginMatch(shifted, counterOriginParams{Reference: scene, Mode: "right_fine"}); !ok {
		t.Fatalf("fresh lateral feedback missing: %v", offsets)
	}
	if _, ok := counterOriginMatch(shifted, counterOriginParams{Reference: scene, Mode: "left_fine"}); ok {
		t.Fatal("opposite correction authorized")
	}
	if _, ok := counterOriginMatch(ref, counterOriginParams{Reference: scene, Mode: "right_fine"}); ok {
		t.Fatal("already aligned pose moved")
	}
	if _, ok := counterOriginMatch(ref, counterOriginParams{Mode: "aligned"}); ok {
		t.Fatal("missing reference accepted")
	}
}

func TestCounterCorrectionUsesOverlappingLandmarkIntervals(t *testing.T) {
	for _, tc := range []struct {
		offsets [2][2]int
		mode    string
	}{
		{[2][2]int{{-2, 0}, {8, 0}}, "right_fine"},
		{[2][2]int{{-8, 0}, {2, 0}}, "left_fine"},
		{[2][2]int{{-8, 0}, {8, 0}}, ""},
		{[2][2]int{{1, 0}, {5, 0}}, ""},
		{[2][2]int{{68, -4}, {66, 0}}, "right_large"},
	} {
		if got := counterCorrectionMode(tc.offsets); got != tc.mode {
			t.Fatalf("%v: got %s want %s", tc.offsets, got, tc.mode)
		}
	}
}
