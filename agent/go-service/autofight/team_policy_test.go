package autofight

import "testing"

func fixtureTeamPolicy() *teamPolicy {
	return &teamPolicy{profile: teamProfile{DriverSlot: 1, Rules: []teamRule{
		{ID: "return", Action: "switch", Slot: 1, Requires: []string{"safe"}},
		{ID: "si", Action: "skill", Slot: 2, MinimumEnergy: 1, Requires: []string{"si_base"}},
		{ID: "burst", Action: "skill", Slot: 1, MinimumEnergy: 1, Requires: []string{"four_flames", "link"}},
		{ID: "detonate", Action: "skill", Slot: 2, Requires: []string{"si_final", "safe_detonation"}},
	}}, fired: map[string]bool{}}
}

func TestTeamPolicyDoesNotGuessUnknownSiPhase(t *testing.T) {
	p := fixtureTeamPolicy()
	f := teamFrame{Valid: true, Selected: 1, Healthy: []int{1}, Energy: 3, Enemy: true, Signals: map[string]bool{}}
	if _, ok := p.next(f); ok {
		t.Fatal("unknown HUD state must not cast any skill")
	}
	f.Signals["si_final"] = true
	if _, ok := p.next(f); ok {
		t.Fatal("final stroke must not detonate without its safety gate")
	}
}

func TestTeamPolicyDoesNotRetryUnchangedSkillState(t *testing.T) {
	p := fixtureTeamPolicy()
	f := teamFrame{Valid: true, Selected: 1, Healthy: []int{1}, Energy: 3, Enemy: true, Signals: map[string]bool{"si_base": true}}
	if r, ok := p.next(f); !ok || r.ID != "si" {
		t.Fatal("expected initial Si skill")
	}
	if _, ok := p.next(f); ok {
		t.Fatal("must wait for observed state change after a cast")
	}
	f.Signals["si_base"] = false
	p.next(f)
	f.Signals["si_base"] = true
	if _, ok := p.next(f); !ok {
		t.Fatal("new observed skill cycle should rearm")
	}
}

func TestTeamPolicyReservesEnergyAndRequiresLink(t *testing.T) {
	p := fixtureTeamPolicy()
	f := teamFrame{Valid: true, Selected: 1, Healthy: []int{1}, Energy: 0, Enemy: true, Signals: map[string]bool{"four_flames": true, "link": true}}
	if _, ok := p.next(f); ok {
		t.Fatal("must not cast without SP")
	}
	f.Energy = 1
	f.Signals["link"] = false
	if _, ok := p.next(f); ok {
		t.Fatal("must wait for support setup")
	}
	f.Signals["link"] = true
	if r, ok := p.next(f); !ok || r.ID != "burst" {
		t.Fatal("expected buffed enhanced skill")
	}
}

func TestTeamPolicyDoesNotReturnToDangerousCarry(t *testing.T) {
	p := fixtureTeamPolicy()
	f := teamFrame{Valid: true, Selected: 3, Enemy: true, Healthy: []int{2, 3, 4}, Signals: map[string]bool{"safe": true}}
	if _, ok := p.next(f); ok {
		t.Fatal("dangerous carry must remain off field")
	}
	f.Healthy = append(f.Healthy, 1)
	if r, ok := p.next(f); !ok || r.Action != "switch" || r.Slot != 1 {
		t.Fatal("expected safe return to carry")
	}
}

func TestTeamPolicyHonorsDisabledSkillsAndInvalidFrames(t *testing.T) {
	p := fixtureTeamPolicy()
	f := teamFrame{Valid: true, Selected: 1, Healthy: []int{1}, Energy: 3, Enemy: true, Signals: map[string]bool{"si_base": true}, Allowed: map[string]bool{"skill": false}}
	if _, ok := p.next(f); ok {
		t.Fatal("disabled skills must not be cast")
	}
	f.Allowed["skill"] = true
	f.Valid = false
	if _, ok := p.next(f); ok {
		t.Fatal("failed recognition must not produce actions")
	}
}

func TestTeamPolicyRejectsConflictingSkillStates(t *testing.T) {
	p := fixtureTeamPolicy()
	p.profile.ExclusiveSignals = [][]string{{"si_base", "si_final"}}
	f := teamFrame{Valid: true, Selected: 1, Healthy: []int{1}, Energy: 3, Enemy: true, Signals: map[string]bool{"si_base": true, "si_final": true, "safe_detonation": true}}
	if _, ok := p.next(f); ok {
		t.Fatal("contradictory phase detections must stop the rotation")
	}
}

func TestTeamProfileRequiresCalibratedSignals(t *testing.T) {
	p := teamProfile{Members: []string{"Laevatain", "Si", "Camille", "Wulfgard"}, DriverSlot: 1, RequiredSignals: []string{"phase"}, Rules: []teamRule{{ID: "si", Action: "skill", Slot: 2, Requires: []string{"phase"}}}}
	if validateTeamProfile(p) == nil {
		t.Fatal("unmapped phase detector must block activation")
	}
	p.Signals = map[string]string{"phase": "verified_detector"}
	if err := validateTeamProfile(p); err != nil {
		t.Fatal(err)
	}
	p.Rules[0].Slot = 5
	if validateTeamProfile(p) == nil {
		t.Fatal("invalid team slot must fail validation")
	}
}

func TestTeamPolicyHoldsAttackOnlyForVerifiedHealthyCarry(t *testing.T) {
	p := fixtureTeamPolicy()
	p.profile.IdentitySignal = "identity"
	f := teamFrame{Valid: true, Selected: 1, Enemy: true, Healthy: []int{1}, Signals: map[string]bool{"identity": true}}
	if !p.shouldAttack(f) {
		t.Fatal("verified carry should keep basic attack held")
	}
	f.Selected = 2
	if p.shouldAttack(f) {
		t.Fatal("support must not inherit the held attack")
	}
	f.Selected = 1
	f.Signals["identity"] = false
	if p.shouldAttack(f) {
		t.Fatal("unknown team identity must release basic attack")
	}
	f.Signals["identity"] = true
	f.Healthy = nil
	if p.shouldAttack(f) {
		t.Fatal("dangerous carry must release attack for recovery")
	}
}
