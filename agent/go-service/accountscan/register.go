package accountscan

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register exposes the read-only account evidence capture action.
func Register() {
	maa.AgentServerRegisterCustomAction("AccountScanRosterStepAction", &BatchAction{})
	maa.AgentServerRegisterCustomAction("AccountScanCaptureAction", &Action{})
}
