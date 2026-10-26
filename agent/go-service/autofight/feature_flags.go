package autofight

// combatExtensions owns fork-only options. Anonymous embedding preserves the
// existing flat JSON settings while upstream fields remain in autoFightAttach.
type combatExtensions struct {
	CombatMode                string `json:"combat_mode"`
	VerifiedTargetAcquisition bool   `json:"verified_target_acquisition"`
	MobileApproach            bool   `json:"mobile_approach"`
	ConfirmedTimeline         bool   `json:"confirmed_timeline"`
	CurrentComboChecks        bool   `json:"current_combo_checks"`
	CombatTrace               bool   `json:"combat_trace"`
	TeamProfile               string `json:"team_profile"`
	RotationPreview           bool   `json:"rotation_preview"`
	RotationMovement          string `json:"rotation_movement"`
}

// Explicit false overrides the defaults during JSON decoding. Existing combat
// action settings retain their previous zero/default handling.
func defaultCombatExtensions() autoFightAttach {
	return autoFightAttach{combatExtensions: combatExtensions{VerifiedTargetAcquisition: true, MobileApproach: true, ConfirmedTimeline: true, CurrentComboChecks: true, CombatTrace: true}}
}

// Routine clears use the active baseline cycle rather than guide readiness gates.
func (p *autoFightAttach) applyCombatMode() {
	if p.CombatMode != "hard_clear" {
		return
	}
	p.VerifiedTargetAcquisition, p.EnableLockTarget = false, false
	p.EnableAttack, p.EnableCombo, p.EnableSkill, p.EnableEndSkill = true, true, true, true
	p.EnableDodge, p.EnableHealthDangerousSwitch, p.EnableBreakAccumulatingPower = true, true, true
	p.ReserveSkillLevel = 0
	p.TeamProfile, p.EndAxisTimelineCode = "", ""
	p.RotationPreview = false
	p.CurrentComboChecks = true
}
