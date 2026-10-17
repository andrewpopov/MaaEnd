package autofight

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
	"time"
)

func TestCombatContinuesWhileCombosCoolDownAndLockIsPending(t *testing.T) {
	t.Setenv("PI_CONTROLLER", `{"name":"ADB","type":"Adb"}`)
	// Detections from the English MuMu battle HUD; deliberately no ready combo or lock label.
	sa := &ScreenAnalyzer{frames: []screenFrame{{Timestamp: time.Now(), Detections: []screenDetection{
		{Box: maa.Rect{905, 55, 19, 17}, Label: LabelCharacterSelect},
		{Box: maa.Rect{502, 629, 91, 14}, Label: LabelEnergyLevelFull},
		{Box: maa.Rect{712, 315, 23, 18}, Label: LabelEnemyTarget},
	}}}}
	if len(sa.GetCharacterComboFull()) != 0 {
		t.Fatal("fixture must have no ready combo")
	}
	if !sa.CombatReady() {
		t.Fatal("combo cooldown must not block held attack and the skill rotation")
	}
	if sa.GetEnemyLocked() {
		t.Fatal("fixture must have no lock-on confirmation")
	}
	if !sa.CombatTargetVisible(true) {
		t.Fatal("visible enemy must allow skills before lock-on confirmation")
	}
	sa.frames[0].Detections = sa.frames[0].Detections[:2]
	if sa.CombatTargetVisible(true) {
		t.Fatal("no enemy detected: targeted skills must wait")
	}
	sa.frames[0].Detections = append(sa.frames[0].Detections, screenDetection{Label: LabelCharacterLevel})
	if sa.CombatReady() {
		t.Fatal("exploration level display must prevent combat entry")
	}
}
