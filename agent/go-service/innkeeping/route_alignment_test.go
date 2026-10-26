package innkeeping

import (
	"image"
	"image/color"
	"math/rand"
	"os"
	"testing"
)

func TestAlignmentUsesMeasuredDirectionAndBoundsEachCorrection(t *testing.T) {
	a := routeAlignment{Command: movementCommand{Kind: "walk", X: 275, Y: 551, Milliseconds: 20, HoldMilliseconds: 80}, Offsets: [2][2]int{{-14, 0}, {-18, 2}}}
	if !validRouteAlignment(&a) {
		t.Fatal("valid measured pulse rejected")
	}
	for _, tc := range []struct {
		offsets [2][2]int
		x       int
	}{{[2][2]int{{12, 0}, {18, -2}}, 275}} {
		cmd, _, ok := alignmentCorrection(tc.offsets, a)
		if !ok || cmd.X != tc.x || cmd.Y != 551 || cmd.Milliseconds != 20 || cmd.HoldMilliseconds != a.Command.HoldMilliseconds || !cmd.Pipeline {
			t.Fatalf("incorrect bounded measured correction: %+v, %v", cmd, ok)
		}
	}
	for _, offsets := range [][2][2]int{{{12, 0}, {-18, 2}}, {{0, 10}, {0, 10}}, {{1, 0}, {1, 0}}} {
		if _, _, ok := alignmentCorrection(offsets, a); ok {
			t.Fatalf("conflicting, perpendicular or negligible offset accepted: %v", offsets)
		}
	}
}

func TestAlignmentCanSampleNegativeOffsetsAtReferenceBoundary(t *testing.T) {
	ref := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	shifted := image.NewRGBA(ref.Bounds())
	rng := rand.New(rand.NewSource(3))
	for y := 125; y < 220; y++ {
		for x := 480; x < 1120; x++ {
			r := uint8(70 + rng.Intn(90))
			ref.SetRGBA(x, y, color.RGBA{r, r - 10, r - 20, 255})
			shifted.SetRGBA(x-12, y-12, color.RGBA{r + 8, r - 2, r - 12, 255})
		}
	}
	var scene sceneFingerprint
	for i, x := range []int{510, 850} {
		scene[i] = sampleScene(ref, [4]int{x, 150, 120, 40})
		scene[i].ArchitectureOnly = true
	}
	offsets, ok := locateRouteScene(shifted, scene)
	if !ok || offsets != [2][2]int{{-12, -12}, {-12, -12}} {
		t.Fatalf("translated patches outside reference-selection bounds or lighting drift rejected: %v, %v", offsets, ok)
	}
}

func TestNearbyAlignmentDoesNotRelaxTheDepartureGate(t *testing.T) {
	f, err := os.Open("testdata/inn-route-counter-reference.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ref, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	shifted := image.NewRGBA(ref.Bounds())
	for y := 0; y < 720; y++ {
		for x := 0; x < 1268; x++ {
			shifted.Set(x+12, y, ref.At(x, y))
		}
	}
	r := fixedRoute{Start: fingerprint(ref), Alignment: &routeAlignment{Command: movementCommand{Kind: "walk", X: 275, Y: 551, Milliseconds: 20, HoldMilliseconds: 80}, Offsets: [2][2]int{{-14, 0}, {-18, 0}}}}
	if sceneMatchesImage(shifted, r.Start) {
		t.Fatal("strict departure gate accepted the 12px offset")
	}
	cmd, _, ok := planRouteAlignment(shifted, r)
	if !ok || cmd.X != 275 {
		t.Fatalf("nearby scene did not produce the measured rightward correction: %+v %v", cmd, ok)
	}
	if !transitDepartureMatches(nil, shifted, r) {
		t.Fatal("measured transit alignment was unreachable at route selection")
	}
	if sceneMatchesImage(shifted, r.Start) {
		t.Fatal("selecting an alignment candidate authorized travel before correction")
	}
	r.Alignment = nil
	if transitDepartureMatches(nil, shifted, r) {
		t.Fatal("transit selection accepted an offset without measured correction")
	}
	if routeDepartureNearby(shifted, r) {
		t.Fatal("uncalibrated route accepted offset departure")
	}
}

func TestAlignmentRejectsPredictedOvershootAndUnmeasuredReverse(t *testing.T) {
	a := routeAlignment{Command: movementCommand{Kind: "walk", X: 275, Y: 551, Milliseconds: 20, HoldMilliseconds: 80}, Offsets: [2][2]int{{-20, 0}, {-20, 0}}}
	for _, offset := range []int{8, -20} {
		if _, _, ok := alignmentCorrection([2][2]int{{offset, 0}, {offset, 0}}, a); ok {
			t.Fatalf("overshoot or unmeasured reverse sent for error %d", offset)
		}
	}
	if _, _, ok := alignmentCorrection([2][2]int{{20, 0}, {20, 0}}, a); !ok {
		t.Fatal("measured convergent pulse rejected")
	}
}
