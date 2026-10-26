package charactercontroller

import (
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const joystickContact = 8

type joystickPort interface {
	down() bool
	move(x, y int) bool
	up() bool
}

type nativeJoystick struct{ ctrl *maa.Controller }

func (j nativeJoystick) down() bool {
	return j.ctrl.PostTouchDown(joystickContact, 195, 551, 0).Wait().Success()
}
func (j nativeJoystick) move(x, y int) bool {
	return j.ctrl.PostTouchMove(joystickContact, int32(x), int32(y), 0).Wait().Success()
}
func (j nativeJoystick) up() bool { return j.ctrl.PostTouchUp(joystickContact).Wait().Success() }

// HoldJoystick executes one freshly authorized mobile movement pulse. It uses the
// navigator's native-touch pattern and contact 8, but never retains a borrowed
// controller or held finger across callbacks. The caller owns screen recognition.
func HoldJoystick(ctrl *maa.Controller, x, y int, duration time.Duration, stopping func() bool) bool {
	if ctrl == nil {
		return false
	}
	return holdJoystick(nativeJoystick{ctrl}, x, y, duration, stopping, time.Sleep)
}

func holdJoystick(j joystickPort, x, y int, duration time.Duration, stopping func() bool, wait func(time.Duration)) (ok bool) {
	if x < 115 || x > 275 || y < 471 || y > 631 || x == 195 && y == 551 || duration <= 0 || duration > 1500*time.Millisecond || stopping == nil || stopping() {
		return false
	}
	// Release even after a failed input: the transport may have partially applied it.
	defer func() { released := j.up(); ok = ok && released }()
	if !j.down() {
		return false
	}
	// Let the game observe neutral contact even when its render/input loop is
	// running at 30 FPS. Short neutral touches can become a recentered joystick.
	wait(50 * time.Millisecond)
	if stopping() || !j.move(x, y) {
		return false
	}
	for remaining := duration; remaining > 0; {
		if stopping() {
			return false
		}
		step := min(remaining, 20*time.Millisecond)
		wait(step)
		remaining -= step
	}
	return !stopping()
}
