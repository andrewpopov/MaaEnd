package autofight

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
	"time"
)

func TestConfiguredInterruptReservesSPWithoutOverridingLargerReserve(t *testing.T) {
	p := autoFightAttach{SharedInterrupts: true, EnableSkill: true, EnableBreakAccumulatingPower: true}
	if p.damageSkillReserve() != 0 {
		t.Fatal("unconfigured skill cannot be assumed to interrupt")
	}
	p.InterruptSkillSlots = []int{2}
	if p.damageSkillReserve() != 1 {
		t.Fatal("damage can consume the last interrupt SP")
	}
	p.ReserveSkillLevel = 2
	if p.damageSkillReserve() != 2 {
		t.Fatal("larger user reserve was reduced")
	}
	p.SharedInterrupts = false
	p.ReserveSkillLevel = 0
	if p.damageSkillReserve() != 0 {
		t.Fatal("disabled interrupt still reserves SP")
	}
}

func TestFreshTelegraphsSuspendDamageUntilCleared(t *testing.T) {
	p := autoFightAttach{EnableDodge: true, SharedInterrupts: true, EnableSkill: true, EnableBreakAccumulatingPower: true, InterruptSkillSlots: []int{2}}
	sa := &ScreenAnalyzer{frames: []screenFrame{{Detections: []screenDetection{warningAt(100)}}}}
	if !sa.defensiveThreat(p) {
		t.Fatal("dodge warning did not suspend damage")
	}
	// A persistent warning remains a threat after dodge attempts are exhausted.
	var dodge dodgeState
	now := time.Unix(100, 0)
	cue := dodge.observe(sa.dodgeWarnings(false), now)
	dodge.dispatched(cue, now, true)
	cue = dodge.observe(sa.dodgeWarnings(false), now.Add(time.Second))
	dodge.dispatched(cue, now.Add(time.Second), true)
	if dodge.observe(sa.dodgeWarnings(false), now.Add(2*time.Second)) != nil || !sa.defensiveThreat(p) {
		t.Fatal("exhausted dodge attempts resumed damage inside a warning")
	}
	sa.frames = append(sa.frames, screenFrame{Detections: []screenDetection{{Label: LabelEnemyAccumPower}}})
	if !sa.defensiveThreat(p) {
		t.Fatal("charge cue did not suspend damage")
	}
	sa.frames = append(sa.frames, screenFrame{})
	if sa.defensiveThreat(p) {
		t.Fatal("stale warning still blocks damage")
	}
}

func TestDefenseReleasesBasicAttackBeforeDodge(t *testing.T) {
	e := &combatExecution{dodgeEnabled: true}
	input := &combatInputFixture{}
	sa := &ScreenAnalyzer{frames: []screenFrame{{Detections: []screenDetection{warningAt(100)}}}}
	e.pauseOffense(input, true)
	if !e.defend(input, sa, time.Now()) {
		t.Fatal("dodge not dispatched")
	}
	if len(input.inputs) != 2 || input.inputs[0] != "__AutoFightActionAttackTouchUp" || input.inputs[1] != "__AutoFightActionDodge" {
		t.Fatal(input.inputs)
	}
}

func TestLastSPAvailableToInterruptButNotQueuedDamage(t *testing.T) {
	savedQueue, savedAnalyzer := actionQueue, screenAnalyzer
	t.Cleanup(func() { actionQueue = savedQueue; screenAnalyzer = savedAnalyzer })
	screenAnalyzer = &ScreenAnalyzer{frames: []screenFrame{{Detections: []screenDetection{{Label: LabelEnergyLevelFull, Box: energyRegions[0]}}}}}
	for _, interrupt := range []bool{false, true} {
		actionQueue = []fightAction{{action: ActionSkill2, interrupt: interrupt}}
		input := &combatInputFixture{definition: nil}
		// The interrupt uses a normal Click action, retaining its reserved SP.
		input.definition = &maa.Node{Action: maa.ActClick(maa.ClickParam{})}
		e := &combatExecution{damageReserve: 1}
		drainActionQueue(input, nil, true, e)
		if (len(input.inputs) == 1) != interrupt {
			t.Fatalf("interrupt=%v inputs=%v", interrupt, input.inputs)
		}
	}
}

func TestReservedSPRejectionDoesNotLeaveTimelinePending(t *testing.T) {
	e := &combatExecution{rejectedSkill: 2}
	timeline := &timelineExecution{pending: true, kind: "skill", slot: 3, confirmations: 1}
	team := &teamPolicy{profile: teamProfile{Rotation: &guideRotation{Steps: []rotationStep{{Action: "skill", Slot: 2}}}}, rotation: rotationState{pending: true}}
	e.reconcileRejectedSkill(timeline, team, 3)
	if timeline.pending || team.rotation.pending || e.rejectedSkill != 0 {
		t.Fatal("undispatched reserved skill left a pending acknowledgement")
	}
}
