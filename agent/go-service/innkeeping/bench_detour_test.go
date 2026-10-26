package innkeeping

import (
	"image"
	"testing"
	"time"
)

func TestTableOneBlockageUsesVisibleLeftAisle(t *testing.T) {
	frame := navImage(t, "inn-table-one-blocked")
	now := time.Unix(1000, 0)
	start := now.Add(-time.Minute)
	n := navigationState{Goal: "diner", Stationary: 3, Last: [4]int{402, 234, 106, 44}, LegStarted: start}
	cmd, ok := benchDetour(&n, frame, now)
	if !ok || cmd.Kind != "round_table" || cmd.X != 115 || cmd.Y != 551 || cmd.Milliseconds != 650 {
		t.Fatalf("clear left aisle not used: %+v %v", cmd, ok)
	}
	if n.LegStarted != start {
		t.Fatal("bypass reset the leg deadline")
	}
	n.Stationary = 3
	n.Last = [4]int{402, 234, 106, 44}
	if _, ok := benchDetour(&n, frame, now); ok {
		t.Fatal("blocked leg can repeat its aisle bypass")
	}
}

func TestMirroredBlockageUsesRightAisleAndUnknownFloorStops(t *testing.T) {
	frame := navImage(t, "inn-table-one-blocked")
	mirrored := image.NewRGBA(frame.Bounds())
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			mirrored.Set(x, y, frame.At(1279-x, y))
		}
	}
	n := navigationState{Goal: "diner", Stationary: 3, Last: [4]int{774, 234, 106, 44}}
	cmd, ok := benchDetour(&n, mirrored, time.Unix(1000, 0))
	if !ok || cmd.X != 275 || cmd.Y != 551 {
		t.Fatalf("right-side clear aisle not used: %+v %v", cmd, ok)
	}
	n = navigationState{Goal: "diner", Stationary: 3, Last: [4]int{402, 234, 106, 44}}
	if _, ok := benchDetour(&n, image.NewRGBA(frame.Bounds()), time.Unix(1000, 0)); ok {
		t.Fatal("unrecognized floor allowed an unverified bypass")
	}
	n.Goal = "counter"
	if _, ok := benchDetour(&n, frame, time.Unix(1000, 0)); ok {
		t.Fatal("diner bypass overrode counter geometry")
	}
}
