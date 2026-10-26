package innkeeping

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"
)

func TestCameraSearchHUDGuardOnActualFrames(t *testing.T) {
	now := time.Unix(1000, 0)
	view := liveFixture(t, "inn-fin-held")
	frame := navImage(t, "inn-fin-held")
	search := newCameraSearch(frame, view.AddOrderBox, map[string]bool{"fin_dumpling": true}, false, now)
	if search == nil || !search.matches(frame, now.Add(time.Second)) {
		t.Fatal("verified world HUD rejected")
	}
	if search.matches(navImage(t, "inn-live-k1"), now.Add(time.Second)) {
		t.Fatal("kitchen overlay allowed a camera pan")
	}
	blank := image.NewRGBA(frame.Bounds())
	if search.matches(blank, now.Add(time.Second)) {
		t.Fatal("missing HUD allowed a camera pan")
	}
	draw.Draw(blank, blank.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if search.matches(blank, now.Add(time.Second)) {
		t.Fatal("white modal background matched the HUD text")
	}
	if search.matches(frame, now.Add(3*time.Second)) {
		t.Fatal("stale search continued after pause")
	}
	search.Pans = 4
	if search.matches(frame, now.Add(time.Second)) {
		t.Fatal("burst exceeded its gesture budget")
	}
}

func TestCameraSearchRequiresVerifiedLabelAndOwnsWantedSet(t *testing.T) {
	now := time.Unix(1000, 0)
	frame := navImage(t, "inn-fin-held")
	if newCameraSearch(frame, [4]int{}, map[string]bool{"fin_dumpling": true}, false, now) != nil {
		t.Fatal("unverified HUD seeded search")
	}
	view := liveFixture(t, "inn-fin-held")
	wanted := map[string]bool{"fin_dumpling": true}
	search := newCameraSearch(frame, view.AddOrderBox, wanted, false, now)
	if search == nil {
		t.Fatal("world HUD not recognized")
	}
	delete(wanted, "fin_dumpling")
	if !search.Wanted["fin_dumpling"] {
		t.Fatal("caller mutated a running search's target set")
	}
}
