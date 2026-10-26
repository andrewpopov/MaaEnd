package innkeeping

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"testing"
)

func testRouteInterval() routeInterval {
	return routeInterval{
		Boundaries:       [2][2][2]int{{{0, 0}, {0, 0}}, {{-24, 0}, {-12, -8}}},
		Evidence:         [2]string{"reviewed-boundary-a.png", "reviewed-boundary-b.png"},
		CrossTrackPixels: 4, ProgressDifference: .2,
	}
}

func TestRouteIntervalMeasuredGeometry(t *testing.T) {
	p := testRouteInterval()
	for _, offsets := range [][2][2]int{{{0, 0}, {0, 0}}, {{-12, 0}, {-6, -4}}, {{-24, 0}, {-12, -8}}} {
		if !routeIntervalOffsetsMatch(offsets, &p) {
			t.Fatalf("safe interval rejected: %v", offsets)
		}
	}
	for _, offsets := range [][2][2]int{
		{{-12, 8}, {-6, 4}},   // Cross-track/camera pitch drift.
		{{-24, 0}, {0, 0}},    // Landmarks disagree about progress.
		{{-26, 0}, {-14, -8}}, // Beyond the measured boundary.
		{{2, 0}, {2, 0}},      // Before the measured boundary.
	} {
		if routeIntervalOffsetsMatch(offsets, &p) {
			t.Fatalf("unsafe offset accepted: %v", offsets)
		}
	}
}

func TestRouteIntervalInvalidEvidenceAndBounds(t *testing.T) {
	for _, mutate := range []func(*routeInterval){
		func(p *routeInterval) { p.Evidence[0] = " " },
		func(p *routeInterval) { p.Evidence[1] = p.Evidence[0] },
		func(p *routeInterval) { p.Boundaries[1] = p.Boundaries[0] },
		func(p *routeInterval) { p.Boundaries[1][0] = [2]int{-72, 72} },
		func(p *routeInterval) { p.Boundaries[1][0] = [2]int{-73, 0} },
		func(p *routeInterval) { p.CrossTrackPixels = 7 },
		func(p *routeInterval) { p.CrossTrackPixels = math.NaN() },
		func(p *routeInterval) { p.ProgressDifference = .3 },
		func(p *routeInterval) { p.ProgressDifference = math.NaN() },
	} {
		p := testRouteInterval()
		mutate(&p)
		if validRouteInterval(&p) {
			t.Fatalf("invalid interval accepted: %+v", p)
		}
	}
	if validRouteInterval(nil) {
		t.Fatal("nil interval accepted")
	}
}

func TestRouteIntervalImageGateKeepsLegacyStrict(t *testing.T) {
	ref := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	current := image.NewRGBA(ref.Bounds())
	rng := rand.New(rand.NewSource(918))
	var scene sceneFingerprint
	offsets := [2][2]int{{-12, 0}, {-6, -4}}
	for i, x := range []int{570, 900} {
		for y := 150; y < 230; y++ {
			for xx := x - 30; xx < x+150; xx++ {
				r := uint8(60 + rng.Intn(100))
				c := color.RGBA{r, r - 10, r - 20, 255}
				ref.SetRGBA(xx, y, c)
				current.SetRGBA(xx+offsets[i][0], y+offsets[i][1], c)
			}
		}
		scene[i] = sampleScene(ref, [4]int{x, 170, 120, 40})
		scene[i].ArchitectureOnly = true
	}
	p := testRouteInterval()
	if !routeIntervalMatches(current, scene, &p) {
		t.Fatal("measured noncanonical return rejected")
	}
	if sceneMatchesImage(current, scene) {
		t.Fatal("fixture should fail legacy strict gate")
	}
	if routeIntervalMatches(image.NewRGBA(ref.Bounds()), scene, &p) {
		t.Fatal("missing landmarks accepted")
	}
	scene[1] = scene[0]
	if routeIntervalMatches(current, scene, &p) {
		t.Fatal("same landmark reused twice")
	}
}

func TestFingerprintFindsNarrowJambBesideBrightWindow(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	for _, box := range []image.Rectangle{image.Rect(500, 190, 580, 230), image.Rect(850, 150, 970, 190)} {
		for y := box.Min.Y; y < box.Max.Y; y++ {
			for x := box.Min.X; x < box.Max.X; x++ {
				r := uint8(70 + (x*13+y*7)%100)
				img.SetRGBA(x, y, color.RGBA{r, r - 10, r - 20, 255})
			}
		}
	}
	scene := fingerprint(img)
	if !sceneMatches(scene, scene) || scene[0].Box[1] != 190 || scene[0].Box[2] != 80 {
		t.Fatalf("narrow static jamb not found: %+v", scene[0].Box)
	}
}

func TestRouteIntervalExplicitSettlingTolerance(t *testing.T) {
	p := routeInterval{Boundaries: [2][2][2]int{{{0, 0}, {0, 0}}, {{-28, 0}, {-34, -2}}}, Evidence: [2]string{"reference.png", "return.png"}, CrossTrackPixels: 4, ProgressDifference: .2}
	measured := [2][2]int{{-30, 0}, {-38, 0}}
	if routeIntervalOffsetsMatch(measured, &p) {
		t.Fatal("legacy endpoint bounds changed")
	}
	p.EndpointTolerancePixels = 4
	if routeIntervalOffsetsMatch(measured, &p) {
		t.Fatal("four-pixel radius accepted a 4.47px residual")
	}
	p.EndpointTolerancePixels = 5
	if !routeIntervalOffsetsMatch(measured, &p) {
		t.Fatal("measured idle settling rejected")
	}
	for _, offsets := range [][2][2]int{{{-36, 0}, {-38, 0}}, {{-30, 0}, {0, 0}}, {{-34, 4}, {-38, 0}}} {
		if routeIntervalOffsetsMatch(offsets, &p) {
			t.Fatalf("beyond settling radius or inconsistent pose accepted: %v", offsets)
		}
	}
	p.EndpointTolerancePixels = 7
	if validRouteInterval(&p) {
		t.Fatal("unbounded settling tolerance accepted")
	}
}
