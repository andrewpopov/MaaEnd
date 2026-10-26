package innkeeping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// ownedInnStartNode proves this task clicked the native menu's session-start control.
func ownedInnStartNode(n *maa.NodeDetail) bool {
	if n == nil || n.Name != "InnkeepingMenuStart" || !n.RunCompleted || n.Action == nil || !n.Action.Success || n.Recognition == nil || !n.Recognition.Hit {
		return false
	}
	if n.Action.Action != "LongPress" && n.Action.Action != "Click" {
		return false
	}
	b := n.Action.Box
	x, y := b[0]+b[2]/2, b[1]+b[3]/2
	return x >= 960 && x <= 1260 && y >= 600 && y <= 690
}

func taskStartedInn(t *maa.Tasker, id int64) bool {
	detail, err := t.GetTaskDetail(id)
	if err != nil || detail == nil {
		return false
	}
	for _, ref := range detail.Nodes {
		n, e := ref.GetDetail()
		if e == nil && ownedInnStartNode(n) {
			return true
		}
	}
	return false
}
