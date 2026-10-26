package autofight

import (
	"fmt"
	"slices"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type combatActionContext interface {
	GetNode(string) (*maa.Node, error)
	RunAction(string, maa.Rect, string, ...any) (*maa.ActionDetail, error)
}

type combatHold struct {
	down, up []*maa.Action
	until    time.Time
	duration time.Duration
	node     string
}

// Long inputs are owned by this callback, but never block its observation loop.
type combatExecution struct {
	rejectedSkill             int
	damageReserve             int
	cancelledUltimate         int
	trace                     *combatTrace
	dodgeEnabled, dodgeCompat bool
	dodge                     dodgeState
	hold                      *combatHold
	waitUntil                 time.Time
	fault                     error
}

func splitCombatHold(action *maa.Action) *combatHold {
	if action == nil {
		return nil
	}
	h := &combatHold{}
	switch p := action.Param.(type) {
	case *maa.LongPressParam:
		h.duration = p.Duration
		h.down = []*maa.Action{maa.ActTouchDown(maa.TouchDownParam{Target: p.Target, TargetOffset: p.TargetOffset, Contact: p.Contact})}
		h.up = []*maa.Action{maa.ActTouchUp(p.Contact)}
	case *maa.LongPressKeyParam:
		h.duration = p.Duration
		for _, key := range p.Key {
			h.down = append(h.down, maa.ActKeyDown(key))
			h.up = append([]*maa.Action{maa.ActKeyUp(key)}, h.up...)
		}
	default:
		return nil
	}
	if h.duration <= 0 {
		h.duration = time.Second
	}
	return h
}

func runCombatInput(ctx combatActionContext, action *maa.Action) bool {
	const node = "__AutoFightInterruptibleInput"
	detail, err := ctx.RunAction(node, maa.Rect{600, 320, 80, 80}, "", map[string]any{
		node: map[string]any{"action": action, "pre_delay": 0, "post_delay": 0, "auto_up": true},
	})
	return err == nil && detail != nil && detail.Success
}

func (e *combatExecution) release(ctx combatActionContext) {
	if e.hold == nil {
		return
	}
	h := e.hold
	released := true
	for _, action := range h.up {
		if !runCombatInput(ctx, action) {
			released = false
		}
	}
	e.trace.event("hold_released", map[string]any{"node": h.node, "controller_accepted": released})
	if !released {
		e.fault = fmt.Errorf("cannot release combat hold %s", h.node)
		return // Keep ownership so callback cleanup can attempt release again.
	}
	e.hold = nil
}

func (e *combatExecution) busy(ctx combatActionContext, now time.Time) bool {
	if e.hold != nil && !now.Before(e.hold.until) {
		e.release(ctx)
	}
	return e.fault == nil && (e.hold != nil || now.Before(e.waitUntil))
}

func (e *combatExecution) run(ctx combatActionContext, node string) (bool, error) {
	definition, err := ctx.GetNode(node)
	if err != nil {
		return false, err
	}
	if definition == nil {
		return false, fmt.Errorf("missing combat action %s", node)
	}
	h := splitCombatHold(definition.Action)
	if h == nil {
		detail, err := ctx.RunAction(node, maa.Rect{600, 320, 80, 80}, "", nil)
		return err == nil && detail != nil && detail.Success, err
	}
	h.node = node
	e.hold = h
	for _, action := range h.down {
		if !runCombatInput(ctx, action) {
			e.release(ctx)
			e.fault = fmt.Errorf("cannot start combat hold %s", node)
			return false, e.fault
		}
	}
	h.until = time.Now().Add(h.duration)
	e.trace.event("hold_started", map[string]any{"node": node, "duration_ms": h.duration.Milliseconds()})
	return true, nil
}

func (e *combatExecution) defend(ctx combatActionContext, sa *ScreenAnalyzer, observedAt time.Time) bool {
	if !e.dodgeEnabled || e.fault != nil {
		return false
	}
	warning := e.dodge.observe(sa.dodgeWarnings(e.dodgeCompat), observedAt)
	if warning == nil {
		return false
	}
	e.preempt(ctx)
	if e.fault != nil {
		return false
	}
	started := time.Now()
	detail, err := ctx.RunAction("__AutoFightActionDodge", maa.Rect{600, 320, 80, 80}, "", nil)
	accepted := err == nil && detail != nil && detail.Success
	e.dodge.dispatched(warning, started, accepted)
	e.trace.event("dodge_dispatch", map[string]any{"label": warning.detection.Label, "box": warning.detection.Box,
		"attempt": warning.attempts, "controller_accepted": accepted, "evasion_confirmed": false,
		"observation_to_dispatch_ms": started.Sub(observedAt).Seconds() * 1000, "error": fmt.Sprint(err)})
	return true
}

// A cancelled hold may already have triggered. Only a subsequent HUD observation
// of an unspent ultimate permits resubmission; consumption still needs acknowledgement.
func (e *combatExecution) reconcileUltimate(f timelineObservation, timeline *timelineExecution, team *teamPolicy) {
	slot := e.cancelledUltimate
	if slot == 0 || !f.hudVisible {
		return
	}
	if slices.Contains(f.ultimates, slot) {
		if timeline.pending && timeline.kind == "ultimate" && timeline.slot == slot {
			timeline.pending = false
			timeline.confirmations = 0
		}
		if team != nil && team.profile.Rotation != nil && team.rotation.pending {
			step := team.profile.Rotation.Steps[team.rotation.cursor]
			if step.Action == "ultimate" && step.Slot == slot {
				team.rotation.pending = false
				team.rotation.confirmationFrames = 0
			}
		}
	}
	e.cancelledUltimate = 0
}

// A stopping task may reject pipeline actions. Release owned inputs directly
// through the controller while the callback's borrowed handles are still valid.
func (e *combatExecution) cleanup(ctx *maa.Context) {
	e.release(ctx)
	if e.hold == nil {
		return
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return
	}
	controller := tasker.GetController()
	if controller == nil {
		return
	}
	released := true
	for _, action := range e.hold.up {
		var job *maa.Job
		switch p := action.Param.(type) {
		case *maa.TouchUpParam:
			job = controller.PostTouchUp(int32(p.Contact))
		case *maa.KeyUpParam:
			job = controller.PostKeyUp(int32(p.Key))
		}
		if job == nil || !job.Wait().Success() {
			released = false
		}
	}
	e.trace.event("hold_cleanup", map[string]any{"node": e.hold.node, "controller_accepted": released})
	if released {
		e.hold = nil
	}
}

// Defence releases an offensive hold even while dodge itself is rate-limited.
func (e *combatExecution) preempt(ctx combatActionContext) {
	if e.hold != nil {
		for slot := 1; slot <= 4; slot++ {
			if e.hold.node == fmt.Sprintf("__AutoFightActionEndSkillOperators%d", slot) {
				e.cancelledUltimate = slot
			}
		}
	}
	e.release(ctx)
	e.waitUntil = time.Time{}
}

func (e *combatExecution) pauseOffense(ctx combatActionContext, attackHeld bool) {
	e.preempt(ctx)
	if !attackHeld || e.fault != nil {
		return
	}
	detail, err := ctx.RunAction("__AutoFightActionAttackTouchUp", maa.Rect{600, 320, 80, 80}, "", nil)
	if err != nil || detail == nil || !detail.Success {
		e.fault = fmt.Errorf("cannot release basic attack for defense: %v", err)
	}
}

// A queued skill rejected before dispatch must not leave an acknowledgement
// pending. Its reserved SP was never spent by this executor.
func (e *combatExecution) reconcileRejectedSkill(timeline *timelineExecution, team *teamPolicy, characterCount int) {
	slot := e.rejectedSkill
	if slot == 0 {
		return
	}
	if timeline.pending && timeline.kind == "skill" && timeline.slot == slot+4-characterCount {
		timeline.pending = false
		timeline.confirmations = 0
	}
	if team != nil && team.profile.Rotation != nil && team.rotation.pending {
		step := team.profile.Rotation.Steps[team.rotation.cursor]
		if step.Action == "skill" && step.Slot == slot {
			team.rotation.pending = false
			team.rotation.confirmationFrames = 0
		}
	}
	e.rejectedSkill = 0
}
