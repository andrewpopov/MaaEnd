package innkeeping

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
)

func TestOwnedInnStartRequiresAcknowledgedCurrentControl(t *testing.T) {
	n := &maa.NodeDetail{Name: "InnkeepingMenuStart", RunCompleted: true, Action: &maa.ActionDetail{Action: "LongPress", Success: true, Box: maa.Rect{1000, 620, 220, 40}}, Recognition: &maa.RecognitionDetail{Hit: true}}
	if !ownedInnStartNode(n) {
		t.Fatal("own successful native start rejected")
	}
	n.Action.Success = false
	if ownedInnStartNode(n) {
		t.Fatal("failed input accepted")
	}
	n.Action.Success = true
	n.Name = "InnkeepingStartTheme"
	if ownedInnStartNode(n) {
		t.Fatal("theme selection confused with session start")
	}
	n.Name = "InnkeepingMenuStart"
	n.Action.Box = maa.Rect{10, 20, 30, 30}
	if ownedInnStartNode(n) {
		t.Fatal("unrelated control accepted")
	}
	n.Action.Box = maa.Rect{1000, 620, 220, 40}
	n.Recognition.Hit = false
	if ownedInnStartNode(n) {
		t.Fatal("missing recognition accepted")
	}
}
