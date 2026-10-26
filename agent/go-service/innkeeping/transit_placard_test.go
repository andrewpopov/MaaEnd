package innkeeping

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"testing"
)

func TestArchitecturePredictedPlacardAcrossInnAndWorld(t *testing.T) {
	ref := navImage(t, "inn-loaded-table5-reference")
	var scene sceneFingerprint
	for i, box := range [][4]int{{690, 160, 80, 40}, {950, 310, 80, 40}} {
		scene[i] = sampleScene(ref, box)
	}
	p := &transitPose{Number: "5", Box: [4]int{728, 307, 30, 40}, Template: "testdata/inn-loaded-table5-board.png"}
	world := navImage(t, "inn-loaded-table5-world")
	if _, matched := locatePhysicalSeatTemplate(world, p.Template, p.Box); matched {
		t.Fatal("fixture no longer exercises low-NCC case")
	}
	box, fallback, ok := transitPoseLocation(world, scene, p)
	if !ok || !fallback {
		t.Fatal("same measured pose did not admit architecture-located placard", box, fallback, ok)
	}
	for _, name := range []string{"inn-table-two-approach", "inn-fin-held", "inn-live-k1"} {
		if _, _, ok := transitPoseLocation(navImage(t, name), scene, p); ok {
			t.Fatal("wrong pose or overlay admitted", name)
		}
	}
}

func TestPlacardRequiresConnectedNumeralBrownFrameAndScale(t *testing.T) {
	for _, name := range []string{"inn-loaded-inner-table5-opening", "inn-loaded-table5-board"} {
		ref := navImage(t, name)
		if !placardMatches(ref, ref) {
			t.Errorf("calibrated placard geometry rejected: %s", name)
		}
	}
	// Dark, thin 1s do not provide a bounded cream glyph. Keep their normal
	// photometric match; they must not acquire the geometry-only fallback.
	for _, name := range []string{"inn-loaded-table1-service", "inn-loaded-inner-row1"} {
		ref := navImage(t, name)
		if placardMatches(ref, ref) {
			t.Errorf("ambiguous dark numeral admitted geometry fallback: %s", name)
		}
	}
	f, err := os.Open("testdata/inn-loaded-table5-board.png")
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !placardMatches(ref, ref) {
		t.Fatal("reference physical board rejected")
	}
	for _, paint := range []color.RGBA{{100, 70, 40, 255}, {230, 200, 150, 255}, {240, 240, 240, 255}} {
		blank := image.NewRGBA(ref.Bounds())
		draw.Draw(blank, blank.Bounds(), image.NewUniform(paint), image.Point{}, draw.Src)
		if placardMatches(blank, ref) {
			t.Fatal("wall or white surface admitted as placard")
		}
	}
	clipped := image.NewRGBA(ref.Bounds())
	// A nearer oversized sign cannot pass by showing a clipped numeral in the ROI.
	for y := 0; y < clipped.Bounds().Dy(); y++ {
		for x := 0; x < clipped.Bounds().Dx(); x++ {
			sx := (x-15)*2/3 + 15
			sy := (y-20)*2/3 + 20
			clipped.Set(x, y, ref.At(sx, sy))
		}
	}
	if placardMatches(clipped, ref) {
		t.Fatal("materially larger sign admitted")
	}
}
