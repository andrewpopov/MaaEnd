package autofight

// Interrupts require an explicitly configured operator, not an arbitrary skill.
func (p autoFightAttach) configuredInterrupt() bool {
	return p.SharedInterrupts && p.EnableBreakAccumulatingPower && p.EnableSkill && len(p.InterruptSkillSlots) > 0
}

func (p autoFightAttach) damageSkillReserve() int {
	reserve := p.ReserveSkillLevel
	if p.configuredInterrupt() && reserve < 1 {
		reserve = 1
	}
	return reserve
}

func (sa *ScreenAnalyzer) defensiveThreat(p autoFightAttach) bool {
	return (p.EnableDodge && len(sa.dodgeWarnings(p.EnableDodgeCompat)) > 0) ||
		(p.configuredInterrupt() && sa.hasLabelInFrames(LabelEnemyAccumPower, 1, false))
}
