package autocollect

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register installs collection history actions and the regular-route cooldown gate.
func Register() {
	maa.AgentServerRegisterCustomAction("AutoCollectHistory", &HistoryAction{})
	maa.AgentServerRegisterCustomRecognition("AutoCollectRouteDue", &RouteDueRecognition{})
}
