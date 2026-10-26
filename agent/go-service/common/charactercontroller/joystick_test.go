package charactercontroller

import (
	"testing"
	"time"
)

type fakeJoystick struct {
	events []string
	fail   string
}

func (j *fakeJoystick) down() bool { j.events = append(j.events, "down"); return j.fail != "down" }
func (j *fakeJoystick) move(x, y int) bool {
	j.events = append(j.events, "move")
	return j.fail != "move"
}
func (j *fakeJoystick) up() bool { j.events = append(j.events, "up"); return j.fail != "up" }
func TestJoystickReleasesOnInputFailure(t *testing.T) {
	for _, fail := range []string{"", "down", "move", "up"} {
		j := &fakeJoystick{fail: fail}
		ok := holdJoystick(j, 195, 471, 100*time.Millisecond, func() bool { return false }, func(time.Duration) {})
		if ok != (fail == "") || len(j.events) == 0 || j.events[len(j.events)-1] != "up" {
			t.Fatalf("%s: %v %+v", fail, ok, j.events)
		}
	}
}
func TestJoystickStopsDuringHeldMovement(t *testing.T) {
	j := &fakeJoystick{}
	elapsed := time.Duration(0)
	ok := holdJoystick(j, 195, 471, time.Second, func() bool { return elapsed >= 56*time.Millisecond }, func(d time.Duration) { elapsed += d })
	if ok || elapsed > 76*time.Millisecond || j.events[len(j.events)-1] != "up" {
		t.Fatalf("stop failed: %v %s %+v", ok, elapsed, j.events)
	}
}
func TestJoystickRejectsUnsafeInputWithoutTouching(t *testing.T) {
	for _, p := range [][3]int{{195, 551, 100}, {100, 471, 100}, {195, 471, 1501}, {195, 471, 0}} {
		j := &fakeJoystick{}
		if holdJoystick(j, p[0], p[1], time.Duration(p[2])*time.Millisecond, func() bool { return false }, func(time.Duration) {}) || len(j.events) > 0 {
			t.Fatalf("invalid pulse accepted: %v", p)
		}
	}
}

func TestJoystickNeutralActivationSurvivesThirtyFPS(t *testing.T) {
	j := &fakeJoystick{}
	waits := []time.Duration{}
	if !holdJoystick(j, 195, 471, 100*time.Millisecond, func() bool { return false }, func(d time.Duration) { waits = append(waits, d) }) {
		t.Fatal("pulse failed")
	}
	if len(waits) == 0 || waits[0] < 34*time.Millisecond {
		t.Fatal("neutral touch can be missed by a 30 FPS frame")
	}
	for _, d := range []string{"forward", "backward", "left", "right"} {
		if _, _, ok := joystickVector(d); !ok {
			t.Fatalf("missing direction %s", d)
		}
	}
	if _, _, ok := joystickVector("unknown"); ok {
		t.Fatal("unknown direction admitted")
	}
}
