package autofight

import "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers all custom recognition and action components for autofight package
func Register() {
	maa.AgentServerRegisterCustomAction("TeamMemberLevelAction", &TeamMemberLevelAction{})
	maa.AgentServerRegisterCustomAction("TeamManagementAction", &TeamManagementAction{})
	maa.AgentServerRegisterCustomAction("TeamCombatCaptureAction", &TeamCombatCaptureAction{})
	maa.AgentServerRegisterCustomAction("TeamCombatPrepareAction", &TeamCombatPrepareAction{})
	maa.AgentServerRegisterCustomRecognition("AutoFightEntryRecognition", &AutoFightEntryRecognition{})
	maa.AgentServerRegisterCustomAction("AutoFightMainAction", &AutoFightMainAction{})
}
