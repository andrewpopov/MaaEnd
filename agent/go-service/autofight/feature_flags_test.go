package autofight

import (
	"encoding/json"
	"testing"
)

func TestCombatExtensionDefaultsAndExplicitOptOut(t *testing.T) {
	p := defaultCombatExtensions()
	if err := json.Unmarshal([]byte(`{}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.VerifiedTargetAcquisition || !p.MobileApproach || !p.ConfirmedTimeline || !p.CurrentComboChecks || !p.CombatTrace {
		t.Fatal("extensions should default on")
	}
	if err := json.Unmarshal([]byte(`{"verified_target_acquisition":false,"confirmed_timeline":false,"current_combo_checks":false,"combat_trace":false}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.VerifiedTargetAcquisition || p.ConfirmedTimeline || p.CurrentComboChecks || p.CombatTrace {
		t.Fatal("explicit opt-out ignored")
	}
}
func TestTimelineFlagPreservesBaselineActionSelection(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		timeline := NewEndAxisTimeline()
		timeline.OrderedCombos = enabled
		timeline.root = &timelineRootRaw{ScenarioList: []timelineScenarioRaw{{Data: timelineDataRaw{Tracks: []timelineTrackRaw{{Actions: []timelineActionRaw{{Type: "skill", StartTime: 300}, {Type: "comboSkill", StartTime: 350}}}}}}}}
		if !timeline.SelectScenario(nil, 4, nil, nil, 3, true) {
			t.Fatal("could not select scenario")
		}
		expected := 1
		if enabled {
			expected = 2
		}
		if len(timeline.queue) != expected {
			t.Fatalf("flag %v: expected %d actions, got %d", enabled, expected, len(timeline.queue))
		}
	}
}

func TestHardClearMode(t *testing.T) {
	p := defaultCombatExtensions()
	p.CombatMode = "hard_clear"
	p.TeamProfile = "guide"
	p.EndAxisTimelineCode = "timeline"
	p.applyCombatMode()
	if p.VerifiedTargetAcquisition || p.EnableLockTarget || p.RotationPreview || p.TeamProfile != "" || p.EndAxisTimelineCode != "" {
		t.Fatal("hard clear can stall")
	}
	if !p.EnableAttack || !p.EnableCombo || !p.EnableSkill || !p.EnableEndSkill || !p.EnableDodge || !p.EnableHealthDangerousSwitch || !p.CurrentComboChecks {
		t.Fatal("missing active combat")
	}
	q := defaultCombatExtensions()
	q.TeamProfile = "guide"
	q.applyCombatMode()
	if !q.VerifiedTargetAcquisition || q.TeamProfile != "guide" {
		t.Fatal("custom mode changed")
	}
}
