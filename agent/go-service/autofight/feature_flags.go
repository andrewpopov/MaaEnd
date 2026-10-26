package autofight

// Explicit false overrides the defaults during JSON decoding. Existing combat
// action settings retain their previous zero/default handling.
func defaultCombatExtensions() autoFightAttach {
	return autoFightAttach{SharedInterrupts: true, VerifiedTargetAcquisition: true, MobileApproach: true, ConfirmedTimeline: true, CurrentComboChecks: true, CombatTrace: true}
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
