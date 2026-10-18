package innkeeping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register exposes the bounded, read-only inn recommendation action.
func Register() { maa.AgentServerRegisterCustomAction("InnkeepingAssistAction", &Action{}) }
