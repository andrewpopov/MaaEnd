package innkeeping

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"
)

func TestWalkBurstRejectsChangedOrdersTrayAndPhase(t *testing.T) {
	now := time.Unix(1000, 0)
	frame := navImage(t, "inn-fin-held")
	v := liveFixture(t, "inn-fin-held")
	v.Prompt = ""
	box := [4]int{600, 180, 70, 45}
	goals := []WorldFood{{Dish: "fin_dumpling", Box: box}}
	w := newWalkBurst(frame, v, movementCommand{Kind: "walk", Dish: "fin_dumpling", Box: box}, goals, false, now)
	if w == nil || !w.matches(frame, now.Add(time.Second)) {
		t.Fatal("fresh unchanged HUD rejected")
	}
	for _, box := range []image.Rectangle{image.Rect(240, 46, 288, 94), image.Rect(459, 599, 507, 647)} {
		changed := image.NewRGBA(frame.Bounds())
		draw.Draw(changed, changed.Bounds(), frame, image.Point{}, draw.Src)
		draw.Draw(changed, box, image.NewUniform(color.RGBA{255, 0, 255, 255}), image.Point{}, draw.Src)
		if w.matches(changed, now.Add(time.Second)) {
			t.Fatal("changed order or tray allowed movement")
		}
	}
	if w.matches(navImage(t, "inn-live-k1"), now.Add(time.Second)) {
		t.Fatal("kitchen allowed movement")
	}
	if w.matches(frame, now.Add(3*time.Second)) {
		t.Fatal("expired burst allowed movement")
	}
	w.Steps = 2
	if w.matches(frame, now.Add(time.Second)) {
		t.Fatal("exhausted burst allowed movement")
	}
}

func TestWalkBurstRequiresUniqueProgressingDistantTarget(t *testing.T) {
	w := &walkBurst{Dish: "fin_dumpling", Last: [4]int{600, 180, 70, 45}}
	goal := WorldFood{Dish: "fin_dumpling", Box: [4]int{610, 185, 74, 48}}
	if _, ok := w.target([]WorldFood{goal}); !ok {
		t.Fatal("nearby progressing target rejected")
	}
	for _, goals := range [][]WorldFood{
		nil,
		{goal, {Dish: goal.Dish, Box: [4]int{620, 190, 74, 48}}},
		{{Dish: "fin_dumpling", Box: w.Last}},
		{{Dish: "fin_dumpling", Box: [4]int{610, 300, 74, 48}}},
		{{Dish: "fin_dumpling", Box: [4]int{900, 185, 74, 48}}},
		{{Dish: "shroom_dumpling", Box: goal.Box}},
	} {
		if _, ok := w.target(goals); ok {
			t.Fatal("lost, ambiguous, stationary or near-arrival target allowed movement", goals)
		}
	}
	frame := navImage(t, "inn-fin-held")
	v := liveFixture(t, "inn-fin-held")
	v.Prompt = "serve"
	if newWalkBurst(frame, v, movementCommand{Kind: "walk", Dish: goal.Dish, Box: goal.Box}, []WorldFood{goal}, false, time.Now()) != nil {
		t.Fatal("interaction seeded walking burst")
	}
	v.Prompt = ""
	if newWalkBurst(frame, v, movementCommand{Kind: "walk", Dish: goal.Dish, Box: goal.Box}, []WorldFood{{Dish: "shroom_dumpling", Box: goal.Box}, goal}, false, time.Now()) == nil {
		t.Fatal("two-dish table rejected chosen navigation dish")
	}
}
