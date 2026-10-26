package innkeeping

import (
	"image"
	"os"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

func TestSeatMarkerRequiresExactConfidentPhysicalNumber(t *testing.T) {
	if !validSeatMarker([4]int{525, 305, 30, 42}) {
		t.Fatal("calibrated table board rejected")
	}
	for _, box := range [][4]int{{525, 20, 30, 42}, {421, 273, 31, 22}, {725, 400, 300, 42}, {0, 0, 0, 0}} {
		if validSeatMarker(box) {
			t.Fatal("HUD, quest, interaction or absent region accepted")
		}
	}
	if !confirmsSeatMarker([]screenocr.Token{{Text: "1", Score: .999}}, "1") {
		t.Fatal("actual high-confidence numeral rejected")
	}
	for _, tokens := range [][]screenocr.Token{{{Text: "3", Score: .999}}, {{Text: "1", Score: .94}}, {{Text: "0/1", Score: .999}}, {{Text: "1", Score: .999}, {Text: "3", Score: .999}}, nil} {
		if confirmsSeatMarker(tokens, "1") {
			t.Fatal("wrong, uncertain, substring or ambiguous table accepted")
		}
	}
}

func TestLocateActualTableSignAfterLargeForegroundDrift(t *testing.T) {
	load := func(name string) image.Image {
		f, err := os.Open("testdata/" + name + ".png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	source := load("inn-seat1-template-source")
	tpl := minicv.ImageCropRect(minicv.ImageConvertRGBA(source), image.Rect(582, 315, 600, 349))
	box, ok := matchPhysicalSeatTemplate(load("inn-seat1-drift"), tpl, [4]int{582, 305, 30, 42})
	if !ok || box[0] < 665 || box[0] > 715 || box[1] < 295 || box[1] > 330 {
		t.Fatalf("physical table-1 sign lost after observed foreground drift: %v %v", box, ok)
	}
	if _, ok := matchPhysicalSeatTemplate(image.NewRGBA(image.Rect(0, 0, 1280, 720)), tpl, [4]int{582, 305, 30, 42}); ok {
		t.Fatal("plain background accepted as physical table")
	}
}
