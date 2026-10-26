package autofight

import (
	"fmt"
	"slices"
	"time"
)

// Guide rotations describe observed transitions, not a wall-clock input macro.
type guideRotation struct {
	SourceURL     string         `json:"source_url"`
	SourceUpdated string         `json:"source_updated"`
	Variant       string         `json:"variant"`
	Steps         []rotationStep `json:"steps"`
	RepeatFrom    int            `json:"repeat_from"`
}

type rotationStep struct {
	ID            string   `json:"id"`
	ReadySignal   string   `json:"ready_signal,omitempty"`
	Action        string   `json:"action"`
	Slot          int      `json:"slot"`
	Requires      []string `json:"requires"`
	Absent        []string `json:"absent"`
	Confirm       []string `json:"confirm"`
	ConfirmAbsent []string `json:"confirm_absent"`
	MinimumEnergy int      `json:"minimum_energy_bars"`
	Event         bool     `json:"event,omitempty"`
	Attack        bool     `json:"allow_basic_attacks"`
}

type rotationState struct {
	lastFrame          time.Time
	waitArmed          bool
	cursor             int
	pending            bool
	issued             time.Time
	confirmationFrames int
	blocked            string
	baseline           map[string]bool
}

func validateGuideRotation(p teamProfile) error {
	r := p.Rotation
	if r == nil {
		return nil
	}
	if r.SourceURL == "" || r.SourceUpdated == "" || r.Variant == "" || len(r.Steps) == 0 || r.RepeatFrom < 0 || r.RepeatFrom >= len(r.Steps) || p.IdentitySignal == "" {
		return fmt.Errorf("guide rotation needs a source, variant, identity gate, and valid repeat point")
	}
	known := func(signal string) bool {
		return slices.Contains(p.RequiredSignals, signal) || slices.Contains([]string{"ultimate_1_ready", "ultimate_2_ready", "ultimate_3_ready", "ultimate_4_ready"}, signal)
	}
	ids := map[string]bool{}
	for _, s := range r.Steps {
		if s.ID == "" || ids[s.ID] || s.Slot < 1 || s.Slot > 4 || s.MinimumEnergy < 0 || s.MinimumEnergy > 4 || !slices.Contains([]string{"skill", "ultimate", "combo", "switch", "wait"}, s.Action) {
			return fmt.Errorf("invalid guide rotation step %q", s.ID)
		}
		ids[s.ID] = true
		if s.ReadySignal != "" && (s.Action != "ultimate" || !known(s.ReadySignal) || !slices.Contains(s.Requires, s.ReadySignal)) {
			return fmt.Errorf("step %q needs a declared ultimate readiness signal in requires", s.ID)
		}
		if len(s.Requires) == 0 || (s.Action != "wait" && len(s.Confirm)+len(s.ConfirmAbsent) == 0) {
			return fmt.Errorf("step %q requires readiness and an observed acknowledgement", s.ID)
		}
		for _, group := range [][]string{s.Requires, s.Absent, s.Confirm, s.ConfirmAbsent} {
			for _, signal := range group {
				if !known(signal) {
					return fmt.Errorf("step %q uses undeclared signal %q", s.ID, signal)
				}
			}
		}
		for _, signal := range s.Requires {
			if slices.Contains(s.Absent, signal) {
				return fmt.Errorf("contradictory readiness in %q", s.ID)
			}
		}
		for _, signal := range s.Confirm {
			if slices.Contains(s.ConfirmAbsent, signal) {
				return fmt.Errorf("contradictory acknowledgement in %q", s.ID)
			}
		}
	}
	return nil
}

func signalsMatch(f teamFrame, present, absent []string) bool {
	for _, key := range present {
		value, known := f.Signals[key]
		if !known || !value {
			return false
		}
	}
	for _, key := range absent {
		value, known := f.Signals[key]
		if !known || value {
			return false
		}
	}
	return true
}

func (p *teamPolicy) advanceRotation() {
	p.rotation.cursor++
	if p.rotation.cursor == len(p.profile.Rotation.Steps) {
		p.rotation.cursor = p.profile.Rotation.RepeatFrom
	}
	p.rotation.pending = false
	p.rotation.confirmationFrames = 0
	p.rotation.baseline = nil
	p.rotation.waitArmed = false
	p.rotation.lastFrame = time.Time{}
}

// nextRotation emits at most one action. A lost input pauses this sequence rather
// than resending a skill and accidentally consuming SP on a different phase.
func (p *teamPolicy) nextRotation(f teamFrame) (teamRule, bool) {
	state := &p.rotation
	if state.blocked != "" {
		return teamRule{}, false
	}
	now := f.Now
	if now.IsZero() {
		now = time.Now()
	}
	step := p.profile.Rotation.Steps[state.cursor]
	if state.pending {
		if !f.Preview && now.Sub(state.issued) > 8*time.Second {
			state.blocked = "No observed acknowledgement: " + step.ID
			return teamRule{}, false
		}
		// A condition already true at dispatch cannot confirm that an input worked.
		changed := false
		for _, key := range append(slices.Clone(step.Confirm), step.ConfirmAbsent...) {
			if value, known := f.Signals[key]; known && value != state.baseline[key] {
				changed = true
			}
		}
		confirmed := signalsMatch(f, step.Confirm, step.ConfirmAbsent) && changed
		if step.Action == "switch" {
			confirmed = confirmed && f.Selected == step.Slot
		}
		if !now.After(state.lastFrame) {
			return teamRule{}, false
		}
		state.lastFrame = now
		if confirmed && now.After(state.issued) {
			state.confirmationFrames++
		} else {
			state.confirmationFrames = 0
		}
		if state.confirmationFrames >= 2 {
			p.advanceRotation()
		}
		return teamRule{}, false
	}
	if step.Action == "wait" && step.Event {
		known := true
		for _, key := range append(slices.Clone(step.Requires), step.Absent...) {
			if _, ok := f.Signals[key]; !ok {
				known = false
			}
		}
		if known && !signalsMatch(f, step.Requires, step.Absent) {
			state.waitArmed = true
		}
	}
	if !signalsMatch(f, step.Requires, step.Absent) || f.Energy < step.MinimumEnergy {
		return teamRule{}, false
	}
	if step.Action == "wait" {
		if step.Event && !state.waitArmed {
			return teamRule{}, false
		}
		p.advanceRotation()
		return teamRule{}, false
	}
	if !slices.Contains(f.Healthy, step.Slot) {
		return teamRule{}, false
	}
	if f.Allowed != nil && !f.Allowed[step.Action] {
		return teamRule{}, false
	}
	if step.Action == "ultimate" {
		ready := step.ReadySignal
		if ready == "" {
			ready = fmt.Sprintf("ultimate_%d_ready", step.Slot)
		}
		if !f.Signals[ready] {
			return teamRule{}, false
		}
	}
	if step.Action == "combo" && (!f.ComboActive || len(f.ComboSlots) != 1 || !slices.Contains(f.ComboSlots, step.Slot)) {
		return teamRule{}, false
	}
	if step.Action == "switch" && (f.Selected == step.Slot || !slices.Contains(f.Healthy, step.Slot)) {
		return teamRule{}, false
	}
	state.pending = true
	state.issued = now
	state.confirmationFrames = 0
	state.baseline = map[string]bool{}
	for key, value := range f.Signals {
		state.baseline[key] = value
	}
	return teamRule{ID: step.ID, Action: step.Action, Slot: step.Slot, MinimumEnergy: step.MinimumEnergy, Requires: step.Requires}, true
}

// Only the current step and its confirmation need OCR. Identity and exclusivity
// remain mandatory each frame; previous screenshots are never reused as state.
func (p *teamPolicy) rotationSignals() []string {
	if p.profile.Rotation == nil {
		return p.profile.RequiredSignals
	}
	step := p.profile.Rotation.Steps[p.rotation.cursor]
	wanted := []string{p.profile.IdentitySignal}
	for _, group := range [][]string{step.Requires, step.Absent, step.Confirm, step.ConfirmAbsent} {
		wanted = append(wanted, group...)
	}
	for _, group := range p.profile.ExclusiveSignals {
		wanted = append(wanted, group...)
	}
	slices.Sort(wanted)
	return slices.Compact(wanted)
}
