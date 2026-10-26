package innkeeping

import (
	"image"
	"image/color"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestTrayContentsAcknowledgesActualInventoryOnly(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join("testdata", "inn-two-second-selected.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	frame, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int{"fin_dumpling": 1, "hot_sour_soup": 1}
	if !trayContentsMatch(frame, lib, expected) {
		t.Fatal("actual two-slot inventory rejected")
	}
	for _, wrong := range []map[string]int{
		{"shroom_dumpling": 1, "hot_sour_soup": 1},
		{"fin_dumpling": 1},
		{"fin_dumpling": 1, "hot_sour_soup": 1, "shroom_dumpling": 1},
		{}, nil, {"fin_dumpling": 0}, {"fin_dumpling": 6}, {"": 1},
	} {
		if trayContentsMatch(frame, lib, wrong) {
			t.Fatalf("unacknowledged contents accepted: %v", wrong)
		}
	}
}

func TestTrayContentsRejectsUnidentifiedFoodAndInvalidFrame(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int{}
	if trayContentsMatch(nil, lib, expected) || trayContentsMatch(image.NewRGBA(image.Rect(0, 0, 640, 360)), lib, expected) {
		t.Fatal("invalid frame accepted")
	}
	frame := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	if !trayContentsMatch(frame, lib, expected) {
		t.Fatal("empty inventory rejected")
	}
	for y := 595; y < 650; y++ {
		for x := 455; x < 510; x++ {
			frame.Set(x, y, color.RGBA{R: 220, G: 30, B: 10, A: 255})
		}
	}
	if trayContentsMatch(frame, lib, expected) {
		t.Fatal("unidentified colored item accepted as empty")
	}
}

func BenchmarkTrayContents(b *testing.B) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		b.Fatal(err)
	}
	f, err := os.Open(filepath.Join("testdata", "inn-two-second-selected.png"))
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	frame, _, err := image.Decode(f)
	if err != nil {
		b.Fatal(err)
	}
	expected := map[string]int{"fin_dumpling": 1, "hot_sour_soup": 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !trayContentsMatch(frame, lib, expected) {
			b.Fatal("actual held inventory rejected")
		}
	}
}

func TestTrayContentsAcknowledgesActualServedEmptyTray(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join("testdata", "inn-first-serve.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	frame, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if !trayContentsMatch(frame, lib, map[string]int{}) {
		t.Fatal("real post-serve empty tray rejected")
	}
	if trayContentsMatch(frame, lib, map[string]int{"fin_dumpling": 1}) {
		t.Fatal("already served dish acknowledged as still carried")
	}
}
