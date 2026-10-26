package innkeeping

import (
	xdraw "golang.org/x/image/draw"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestCollapsedGuestHeaderDoesNotReadWorldDish(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	ref := lib["hot_sour_soup"]
	if ref == nil {
		t.Fatal("missing soup exemplar")
	}
	frame := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{100, 70, 40, 255}), image.Point{}, draw.Src)
	for seat := 0; seat < 6; seat++ {
		left := 234 + seat*118
		draw.Draw(frame, image.Rect(left, 18, left+116, 40), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	}
	// Put a real soup lookalike in the world below collapsed seat 3.
	left := 234 + 2*118
	box := image.Rect(left+12, 52, left+48, 88)
	xdraw.CatmullRom.Scale(frame, box, ref, ref.Bounds(), draw.Src, nil)
	if id, _ := lib.Identify(frame, left+30, 70, 18); id != "hot_sour_soup" {
		t.Fatalf("negative fixture is not a soup lookalike: %s", id)
	}
	view := readLivePhase(frame, nil, lib, nil, "active")
	if len(view.Seats[2].Dishes) != 0 || view.Seats[2].Phase != "unknown" {
		t.Fatalf("world became an order: %+v", view.Seats[2])
	}
	// The same icon inside an expanded card is a valid request.
	draw.Draw(frame, image.Rect(left, 40, left+116, 120), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(frame, box, ref, ref.Bounds(), draw.Src, nil)
	view = readLivePhase(frame, nil, lib, nil, "active")
	if len(view.Seats[2].Dishes) != 1 || view.Seats[2].Dishes[0] != "hot_sour_soup" {
		t.Fatalf("real order lost: %+v", view.Seats[2])
	}
}
