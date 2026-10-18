package accountscan

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"image"
	"image/color"
	"testing"
)

func TestSelectedPortraitAfterScroll(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 387; y < 462; y++ {
		for x := 71; x < 76; x++ {
			img.Set(x, y, color.RGBA{255, 230, 0, 255})
		}
	}
	box, ok := selectedPortrait(img)
	if !ok || box.Y() != 382 {
		t.Fatalf("selected portrait = %v, %v", box, ok)
	}
	img = image.NewRGBA(img.Bounds())
	for y := 542; y < 577; y++ {
		for x := 71; x < 76; x++ {
			img.Set(x, y, color.RGBA{255, 230, 0, 255})
		}
	}
	if box, ok := selectedPortrait(img); !ok || box.Y() != 537 {
		t.Fatalf("faded bottom selection = %v, %v", box, ok)
	}
	// Short yellow portrait details must not masquerade as the selection bar.
	img = image.NewRGBA(img.Bounds())
	for y := 100; y < 120; y++ {
		for x := 71; x < 76; x++ {
			img.Set(x, y, color.RGBA{255, 230, 0, 255})
		}
	}
	if _, ok := selectedPortrait(img); ok {
		t.Fatal("short artwork accepted as selection")
	}
}

func TestSignatureDetectsListMovement(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	rect := image.Rect(0, 80, 70, 590)
	before := signature(img, rect)
	if !unchanged(before, signature(img, rect)) {
		t.Fatal("stationary rail changed")
	}
	for y := 100; y < 300; y++ {
		for x := 0; x < 70; x++ {
			img.Set(x, y, color.White)
		}
	}
	if unchanged(before, signature(img, rect)) {
		t.Fatal("moved rail accepted as end")
	}
	if unchanged(nil, nil) {
		t.Fatal("missing evidence accepted as end")
	}
}

func TestMatrixNodeRequiresVisibleGlyph(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	roi := image.Rect(940, 145, 1035, 235)
	if _, ok := matrixWhiteNode(img, roi); ok {
		t.Fatal("empty matrix accepted")
	}
	for y := 166; y < 185; y++ {
		for x := 963; x < 982; x++ {
			img.Set(x, y, color.White)
		}
	}
	got, ok := matrixWhiteNode(img, roi)
	if !ok || got.X < 969 || got.X > 975 || got.Y < 172 || got.Y > 178 {
		t.Fatalf("matrix node = %v, %v", got, ok)
	}
}

func TestOverviewNameSurvivesObscuredBreadcrumb(t *testing.T) {
	const id int64 = -401
	batchRuns[id] = &batchRun{Mode: "operators"}
	defer delete(batchRuns, id)
	tokens := []screenocr.Token{
		{Text: "Wulfgard", Score: .99, Box: [4]int{175, 120, 210, 40}},
		{Text: "UID: 1234567890", Score: .99, Box: [4]int{89, 690, 115, 20}},
	}
	name, uid, _, err := batchCaptureContext(id, "overview", tokens)
	if err != nil || name != "Wulfgard" || uid != "1234567890" {
		t.Fatalf("visible name = %q %q %v", name, uid, err)
	}
	if _, _, _, err = batchCaptureContext(id, "overview", tokens); err == nil {
		t.Fatal("unchanged operator was accepted")
	}
}
