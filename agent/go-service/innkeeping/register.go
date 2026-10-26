package innkeeping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register exposes screen recommendations and opt-in reactive inn assistance.
func Register() {
	maa.AgentServerRegisterCustomRecognition("InnkeepingCounterOrigin", &CounterOriginRecognition{})
	maa.AgentServerRegisterCustomRecognition("InnkeepingKitchenQueueCount", &KitchenQueueCountRecognition{})
	maa.AgentServerRegisterCustomRecognition("InnkeepingTrayContents", &TrayContentsRecognition{})
	maa.AgentServerRegisterCustomRecognition("InnkeepingPhysicalBoard", &PhysicalBoardRecognition{})
	maa.AgentServerRegisterCustomRecognition("InnkeepingReadyFoodOffset", &ReadyFoodOffsetRecognition{})
	maa.AgentServerRegisterCustomAction("InnkeepingAssistAction", &Action{})
	reactive := &ReactiveAction{}
	maa.AgentServerRegisterCustomAction("InnkeepingReactiveAction", reactive)
	maa.AgentServerRegisterCustomAction("InnkeepingReactiveClickAction", &ReactiveClickAction{owner: reactive})
	maa.AgentServerRegisterCustomAction("InnkeepingReactiveNavigateAction", &ReactiveNavigateAction{owner: reactive})
	maa.AgentServerRegisterCustomAction("InnkeepingCameraSearchAction", &InnCameraSearchAction{owner: reactive})
	maa.AgentServerRegisterCustomAction("InnkeepingWalkBurstAction", &InnWalkBurstAction{owner: reactive})
	maa.AgentServerRegisterCustomAction("InnkeepingFixedRouteStepAction", &InnFixedRouteStepAction{owner: reactive})
	maa.AgentServerRegisterCustomAction("InnkeepingMenuAction", &InnMenuAction{})
	maa.AgentServerRegisterCustomAction("InnkeepingOpenAction", &InnOpenAction{})
	maa.AgentServerRegisterCustomAction("InnkeepingSetupNavigateAction", &InnSetupNavigateAction{owner: reactive})
}
