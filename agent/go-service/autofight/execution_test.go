package autofight

import (
	"fmt"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type combatInputFixture struct {
	definition *maa.Node
	inputs     []string
	fail       string
}

func (f *combatInputFixture) GetNode(name string) (*maa.Node, error) {
	return f.definition, nil
}
func (f *combatInputFixture) RunAction(name string, box maa.Rect, recognition string, override ...any) (*maa.ActionDetail, error) {
	input := name
	if name == "__AutoFightInterruptibleInput" {
		action := override[0].(map[string]any)[name].(map[string]any)["action"].(*maa.Action)
		input = string(action.Type)
		if p, ok := action.Param.(*maa.KeyDownParam); ok {
			input = fmt.Sprintf("KeyDown:%d", p.Key)
		}
		if p, ok := action.Param.(*maa.KeyUpParam); ok {
			input = fmt.Sprintf("KeyUp:%d", p.Key)
		}
	}
	f.inputs = append(f.inputs, input)
	return &maa.ActionDetail{Success: input != f.fail}, nil
}
func warningAt(x int) screenDetection {
	return screenDetection{Label: LabelEnemyDodge, Box: maa.Rect{x, 200, 40, 40}}
}
func TestDodgeFailureAndUnconfirmedInputHaveBoundedFreshFollowup(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		var state dodgeState
		now := time.Unix(100, 0)
		cues := []screenDetection{warningAt(100)}
		first := state.observe(cues, now)
		if first == nil {
			t.Fatal("new warning ignored")
		}
		// Observation alone must not consume a warning.
		if state.observe(cues, now) != first {
			t.Fatal("undispatched warning consumed")
		}
		state.dispatched(first, now, accepted)
		if first.controllerAccepted != accepted {
			t.Fatal("dispatch result lost")
		}
		if state.observe(cues, now.Add(100*time.Millisecond)) != nil {
			t.Fatal("repeated input too soon")
		}
		second := state.observe(cues, now.Add(dodgeRetryInterval))
		if second != first {
			t.Fatal("unresolved warning must allow one follow-up")
		}
		state.dispatched(second, now.Add(dodgeRetryInterval), true)
		if state.observe(cues, now.Add(5*time.Second)) != nil {
			t.Fatal("persistent warning retried indefinitely")
		}
		state.observe(nil, now.Add(6*time.Second))
		if state.observe(cues, now.Add(7*time.Second)) == nil {
			t.Fatal("new warning episode did not rearm")
		}
	}
}
func TestOverlappingEnemiesDoNotCollapseIntoOneWarning(t *testing.T) {
	var state dodgeState
	now := time.Unix(100, 0)
	first := state.observe([]screenDetection{warningAt(100)}, now)
	state.dispatched(first, now, true)
	second := state.observe([]screenDetection{warningAt(102), warningAt(400)}, now.Add(400*time.Millisecond))
	if second == nil || second == first || second.detection.Box[0] != 400 {
		t.Fatal("new enemy warning suppressed by existing warning")
	}
}
func TestDodgePreemptsUltimateWithoutSelectedPortrait(t *testing.T) {
	input := &combatInputFixture{definition: &maa.Node{Action: maa.ActLongPress(maa.LongPressParam{Target: maa.NewTargetRect(maa.Rect{926, 629, 1, 1}), Contact: 2, Duration: 1500 * time.Millisecond})}}
	execution := &combatExecution{dodgeEnabled: true}
	if ok, err := execution.run(input, "__AutoFightActionEndSkillOperators1"); !ok || err != nil {
		t.Fatal(err)
	}
	if len(input.inputs) != 1 || input.inputs[0] != "TouchDown" {
		t.Fatal("long press blocked or completed synchronously")
	}
	sa := &ScreenAnalyzer{frames: []screenFrame{{Detections: []screenDetection{warningAt(100)}}}}
	if sa.GetCharacterSelect() != 0 {
		t.Fatal("fixture must omit selected portrait")
	}
	if !execution.defend(input, sa, time.Now()) {
		t.Fatal("missing portrait blocked defense")
	}
	want := []string{"TouchDown", "TouchUp", "__AutoFightActionDodge"}
	if fmt.Sprint(input.inputs) != fmt.Sprint(want) {
		t.Fatalf("must release ultimate contact before dodge: %v", input.inputs)
	}
	if execution.hold != nil || execution.cancelledUltimate != 1 {
		t.Fatal("cancelled hold not released or tracked")
	}
}
func TestCombatHoldDeadlineAndCleanupReleaseAllKeys(t *testing.T) {
	input := &combatInputFixture{definition: &maa.Node{Action: maa.ActLongPressKey(maa.LongPressKeyParam{Key: []int{18, 49}, Duration: 1500 * time.Millisecond})}}
	execution := &combatExecution{}
	execution.run(input, "ultimate")
	deadline := execution.hold.until
	if !execution.busy(input, deadline.Add(-time.Millisecond)) {
		t.Fatal("hold ended early")
	}
	if execution.busy(input, deadline) || execution.hold != nil {
		t.Fatal("hold not released at deadline")
	}
	want := []string{"KeyDown:18", "KeyDown:49", "KeyUp:49", "KeyUp:18"}
	if fmt.Sprint(input.inputs) != fmt.Sprint(want) {
		t.Fatal(input.inputs)
	}
	execution.run(input, "ultimate")
	execution.release(input) // Same cleanup used on callback stop/error.
	if execution.hold != nil {
		t.Fatal("stop leaked held keys")
	}
}
func TestFailedHoldReleaseBlocksDodgeUntilCleanup(t *testing.T) {
	input := &combatInputFixture{definition: &maa.Node{Action: maa.ActLongPressKey(maa.LongPressKeyParam{Key: []int{49}})}, fail: "KeyUp:49"}
	execution := &combatExecution{dodgeEnabled: true}
	execution.run(input, "ultimate")
	sa := &ScreenAnalyzer{frames: []screenFrame{{Detections: []screenDetection{warningAt(100)}}}}
	if execution.defend(input, sa, time.Now()) || execution.fault == nil {
		t.Fatal("defense reused an unreleased input")
	}
	if execution.hold == nil {
		t.Fatal("failed release lost ownership")
	}
	input.fail = ""
	execution.release(input)
	if execution.hold != nil {
		t.Fatal("cleanup failed to release input")
	}
}
func TestRecoveryWaitCanBePreemptedByFreshWarning(t *testing.T) {
	execution := &combatExecution{dodgeEnabled: true, waitUntil: time.Now().Add(2 * time.Second)}
	input := &combatInputFixture{}
	sa := &ScreenAnalyzer{frames: []screenFrame{{Detections: []screenDetection{warningAt(100)}}}}
	if !execution.defend(input, sa, time.Now()) || !execution.waitUntil.IsZero() {
		t.Fatal("recovery wait blocked dodge")
	}
}
func TestQueueExecutesOneActionPerObservation(t *testing.T) {
	saved := actionQueue
	t.Cleanup(func() { actionQueue = saved })
	actionQueue = []fightAction{{action: ActionSkill1}, {action: ActionSkill2}}
	input := &combatInputFixture{definition: &maa.Node{Action: maa.ActClick(maa.ClickParam{})}}
	execution := &combatExecution{}
	// Exercise the same single-step dispatcher using its injected context.
	drainActionQueue(input, nil, true, execution)
	if len(input.inputs) != 1 || len(actionQueue) != 1 {
		t.Fatal("offense batch prevented a fresh observation")
	}
}
func TestDodgeCompatibilityKeepsGroundWarnings(t *testing.T) {
	sa := &ScreenAnalyzer{frames: []screenFrame{{Detections: []screenDetection{
		{Label: LabelEnemyDodge, Box: maa.Rect{600, 350, 20, 20}},
		{Label: LabelEnemyAttackGroundDodge, Box: maa.Rect{600, 350, 20, 20}},
	}}}}
	if len(sa.dodgeWarnings(false)) != 2 || len(sa.dodgeWarnings(true)) != 1 {
		t.Fatal("compatibility filtering changed")
	}
}

func TestCancelledUltimateOnlyRearmsWhenResourceStillAvailable(t *testing.T) {
	for _, ready := range []bool{false, true} {
		execution := &combatExecution{cancelledUltimate: 1}
		timeline := &timelineExecution{pending: true, kind: "ultimate", slot: 1, confirmations: 1}
		team := &teamPolicy{profile: teamProfile{Rotation: &guideRotation{Steps: []rotationStep{{Action: "ultimate", Slot: 1}}}}, rotation: rotationState{pending: true, confirmationFrames: 1}}
		execution.reconcileUltimate(timelineObservation{}, timeline, team)
		if !timeline.pending || !team.rotation.pending || execution.cancelledUltimate != 1 {
			t.Fatal("missing HUD must not acknowledge cancellation")
		}
		frame := timelineObservation{hudVisible: true}
		if ready {
			frame.ultimates = []int{1}
		}
		execution.reconcileUltimate(frame, timeline, team)
		if timeline.pending == ready || team.rotation.pending == ready {
			t.Fatal("consumed and unspent ultimates need different recovery")
		}
		if execution.cancelledUltimate != 0 {
			t.Fatal("cancellation not resolved")
		}
	}
}

func TestDisabledDodgeDoesNotInterruptHold(t *testing.T) {
	input := &combatInputFixture{definition: &maa.Node{Action: maa.ActLongPressKey(maa.LongPressKeyParam{Key: []int{49}})}}
	execution := &combatExecution{}
	execution.run(input, "ultimate")
	sa := &ScreenAnalyzer{frames: []screenFrame{{Detections: []screenDetection{warningAt(100)}}}}
	if execution.defend(input, sa, time.Now()) || execution.hold == nil || len(input.inputs) != 1 {
		t.Fatal("disabled dodge changed input")
	}
	execution.release(input)
}
