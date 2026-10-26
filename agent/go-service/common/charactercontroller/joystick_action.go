package charactercontroller

import (
	"encoding/json"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"time"
)

var _ maa.CustomActionRunner = &CharacterControllerJoystickAction{}

// CharacterControllerJoystickAction owns one bounded native touch, including
// neutral activation and release. Pipeline supplies fresh screen recognition.
type CharacterControllerJoystickAction struct{}

func joystickVector(direction string) (int, int, bool) {
	switch direction {
	case "forward":
		return 195, 471, true
	case "backward":
		return 195, 631, true
	case "left":
		return 115, 551, true
	case "right":
		return 275, 551, true
	default:
		return 0, 0, false
	}
}

func (a *CharacterControllerJoystickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || !pienv.IsAdbLikeControllerType(pienv.ControllerType()) {
		return false
	}
	var p struct {
		Direction string `json:"direction"`
		Duration  int    `json:"duration"`
	}
	if json.Unmarshal([]byte(arg.CustomActionParam), &p) != nil || p.Duration <= 0 || p.Duration > 1500 {
		return false
	}
	x, y, ok := joystickVector(p.Direction)
	if !ok {
		return false
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return false
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return false
	}
	return HoldJoystick(ctrl, x, y, time.Duration(p.Duration)*time.Millisecond, tasker.Stopping)
}
