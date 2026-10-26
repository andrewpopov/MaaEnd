package innkeeping

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
)

func TestTrialModeRequiresAcknowledgedNativeSelection(t *testing.T) {
	n := &maa.NodeDetail{Name: "InnCalTheme", RunCompleted: true, Action: &maa.ActionDetail{Action: "LongPress", Success: true, Box: maa.Rect{800, 630, 100, 30}}, Recognition: &maa.RecognitionDetail{Hit: true}}
	if !trialSelectionNode(n) {
		t.Fatal("recorded native Trial selection rejected")
	}
	n.Action.Success = false
	if trialSelectionNode(n) {
		t.Fatal("failed Trial input authorized research routes")
	}
	n.Action.Success = true
	n.Name = "InnkeepingStartReal"
	if trialSelectionNode(n) {
		t.Fatal("real-session selection authorized Trial routes")
	}
	n.Name = "InnCalTheme"
	n.Action.Box = maa.Rect{1000, 630, 100, 30}
	if trialSelectionNode(n) {
		t.Fatal("a different control authorized Trial routes")
	}
}
func TestTrialMiddleMacroCannotBecomeAnotherTravelLane(t *testing.T) {
	r := fixedRoute{Goal: "diner", Steps: []movementCommand{{Kind: "walk", X: 115, Y: 551, Milliseconds: 20, HoldMilliseconds: 600, Pipeline: true}, {Kind: "walk", X: 195, Y: 471, Milliseconds: 20, HoldMilliseconds: 700, Pipeline: true}, {Kind: "walk", X: 275, Y: 551, Milliseconds: 20, HoldMilliseconds: 450, Pipeline: true}}}
	if !trialMiddleSteps(r) {
		t.Fatal("measured middle-lane leg rejected")
	}
	r.Steps[1].X = 275
	if trialMiddleSteps(r) {
		t.Fatal("diagonal shortcut accepted")
	}
}
