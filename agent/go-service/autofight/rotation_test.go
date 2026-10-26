package autofight

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rotationFixture() (*teamPolicy, teamFrame) {
	p := &teamPolicy{profile: teamProfile{DriverSlot: 1, IdentitySignal: "identity", Rotation: &guideRotation{RepeatFrom: 0, Steps: []rotationStep{
		{ID: "setup", Action: "skill", Slot: 2, Requires: []string{"setup_ready"}, Confirm: []string{"setup_applied"}, MinimumEnergy: 1, Attack: true},
		{ID: "burst", Action: "ultimate", Slot: 1, Requires: []string{"setup_applied", "ultimate_1_ready"}, ConfirmAbsent: []string{"ultimate_1_ready"}},
	}}}}
	f := teamFrame{Valid: true, Enemy: true, Selected: 1, Healthy: []int{1, 2, 3, 4}, Energy: 3, Signals: map[string]bool{"identity": true, "setup_ready": true, "setup_applied": false, "ultimate_1_ready": true}, Now: time.Unix(100, 0)}
	return p, f
}
func TestRotationWaitsForAcknowledgement(t *testing.T) {
	p, f := rotationFixture()
	if r, ok := p.next(f); !ok || r.ID != "setup" {
		t.Fatal("missing setup")
	}
	f.Now = f.Now.Add(time.Millisecond * 100)
	if _, ok := p.next(f); ok {
		t.Fatal("unchanged HUD retried input")
	}
	f.Signals["setup_applied"] = true
	for i := 0; i < 2; i++ {
		f.Now = f.Now.Add(time.Millisecond * 100)
		if _, ok := p.next(f); ok {
			t.Fatal("ack frame must not issue next input")
		}
	}
	f.Now = f.Now.Add(time.Millisecond * 100)
	if r, ok := p.next(f); !ok || r.ID != "burst" {
		t.Fatal("confirmed setup did not unlock burst")
	}
}
func TestRotationRejectsAlreadyTrueAcknowledgement(t *testing.T) {
	p, f := rotationFixture()
	f.Signals["setup_applied"] = true
	p.next(f)
	for i := 0; i < 5; i++ {
		f.Now = f.Now.Add(time.Millisecond * 100)
		p.next(f)
	}
	if p.rotation.cursor != 0 {
		t.Fatal("condition predating input cannot acknowledge a cast")
	}
}
func TestRotationLostInputPausesInsteadOfRecasting(t *testing.T) {
	p, f := rotationFixture()
	p.next(f)
	f.Now = f.Now.Add(time.Second * 9)
	if _, ok := p.next(f); ok || p.rotation.blocked == "" {
		t.Fatal("lost cast must pause with a reason")
	}
	f.Signals["setup_applied"] = true
	if _, ok := p.next(f); ok {
		t.Fatal("late frames must not resume a timed-out rotation")
	}
}
func TestRotationRequiresExactComboPrompt(t *testing.T) {
	p, f := rotationFixture()
	p.profile.Rotation.Steps[0].Action = "combo"
	f.ComboActive = true
	f.ComboSlots = []int{3}
	if _, ok := p.next(f); ok {
		t.Fatal("another operator's combo cannot satisfy this step")
	}
	f.ComboSlots = []int{2}
	if _, ok := p.next(f); !ok {
		t.Fatal("visible intended combo should be eligible")
	}
}
func TestRotationHonorsIdentitySPAndUserControls(t *testing.T) {
	for _, mode := range []string{"identity", "energy", "disabled", "invalid"} {
		p, f := rotationFixture()
		switch mode {
		case "identity":
			f.Signals["identity"] = false
		case "energy":
			f.Energy = 0
		case "disabled":
			f.Allowed = map[string]bool{"skill": false}
		case "invalid":
			f.Valid = false
		}
		if _, ok := p.next(f); ok {
			t.Fatal("invalid gate allowed a cast: " + mode)
		}
	}
}
func TestRotationMissingNegativeIsUnknown(t *testing.T) {
	f := teamFrame{Signals: map[string]bool{}}
	if signalsMatch(f, nil, []string{"cooldown"}) {
		t.Fatal("missing OCR evidence must not mean cooldown absent")
	}
}
func TestRotationOnlyObservesCurrentStep(t *testing.T) {
	p, _ := rotationFixture()
	got := p.rotationSignals()
	if len(got) != 3 {
		t.Fatalf("observe identity, readiness and acknowledgement only: %v", got)
	}
}
func TestGuideRotationRequiresAcknowledgedSteps(t *testing.T) {
	p, _ := rotationFixture()
	p.profile.RequiredSignals = []string{"identity", "setup_ready", "setup_applied"}
	p.profile.Rotation.SourceURL = "https://example.org/guide"
	p.profile.Rotation.SourceUpdated = "2026-10-20"
	p.profile.Rotation.Variant = "test"
	if err := validateGuideRotation(p.profile); err != nil {
		t.Fatal(err)
	}
	p.profile.Rotation.Steps[0].Confirm = nil
	if validateGuideRotation(p.profile) == nil {
		t.Fatal("unacknowledged macros must fail validation")
	}
}

func TestRotationDuplicateFramesDoNotConfirmOrEraseProgress(t *testing.T) {
	p, f := rotationFixture()
	p.next(f)
	f.Signals["setup_applied"] = true
	f.Now = f.Now.Add(time.Second)
	p.next(f)
	p.next(f)
	if p.rotation.confirmationFrames != 1 {
		t.Fatal("duplicate frame changed confirmation progress")
	}
	f.Now = f.Now.Add(time.Second)
	p.next(f)
	if p.rotation.cursor != 1 {
		t.Fatal("two distinct frames should confirm")
	}
}
func TestRotationPreviewWaitsForHumanWithoutTimeout(t *testing.T) {
	p, f := rotationFixture()
	f.Preview = true
	p.next(f)
	f.Now = f.Now.Add(time.Minute)
	p.next(f)
	if p.rotation.blocked != "" {
		t.Fatal("preview timed out before human action")
	}
	f.Signals["setup_applied"] = true
	for i := 0; i < 2; i++ {
		f.Now = f.Now.Add(time.Second)
		p.next(f)
	}
	if p.rotation.cursor != 1 {
		t.Fatal("human acknowledgement did not advance preview")
	}
}
func TestRotationEventWaitRequiresNewEdge(t *testing.T) {
	p, f := rotationFixture()
	p.profile.Rotation.Steps[0] = rotationStep{ID: "strike", Action: "wait", Slot: 1, Requires: []string{"strike"}, Event: true}
	f.Signals["strike"] = true
	p.next(f)
	if p.rotation.cursor != 0 {
		t.Fatal("old strike consumed")
	}
	f.Signals["strike"] = false
	p.next(f)
	f.Signals["strike"] = true
	p.next(f)
	if p.rotation.cursor != 1 {
		t.Fatal("new strike not consumed")
	}
}
func TestRotationSequenceUltimateUsesItsOwnReadiness(t *testing.T) {
	p, f := rotationFixture()
	p.rotation.cursor = 1
	s := &p.profile.Rotation.Steps[1]
	s.ReadySignal = "sequence_ready"
	s.Requires = []string{"sequence_ready"}
	s.Confirm = []string{"sequence_used"}
	s.ConfirmAbsent = nil
	f.Signals["ultimate_1_ready"] = false
	f.Signals["sequence_ready"] = true
	f.Signals["sequence_used"] = false
	if _, ok := p.next(f); !ok {
		t.Fatal("sequence ultimate incorrectly requires full energy")
	}
}
func TestGuideRotationSourceProfilesValidate(t *testing.T) {
	files, err := filepath.Glob("../../../assets/data/CombatTeams/guide-*.json")
	if err != nil || len(files) != 3 {
		t.Fatal("missing guide profiles", err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var profile teamProfile
		if err = json.Unmarshal(raw, &profile); err != nil {
			t.Fatal(err)
		}
		if err = validateGuideRotation(profile); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
}

func TestRotationUnknownEventDoesNotArmWait(t *testing.T) {
	p, f := rotationFixture()
	p.profile.Rotation.Steps[0] = rotationStep{ID: "strike", Action: "wait", Slot: 1, Requires: []string{"strike"}, Event: true}
	p.next(f)
	f.Signals["strike"] = true
	p.next(f)
	if p.rotation.cursor != 0 {
		t.Fatal("unknown evidence armed event wait")
	}
}
func TestRotationDoesNotCastForDownedOperator(t *testing.T) {
	p, f := rotationFixture()
	f.Healthy = []int{1, 3, 4}
	if _, ok := p.next(f); ok {
		t.Fatal("cast issued for downed teammate")
	}
}

func TestFinalStrikeWaitKeepsAttackingAndOrdersNextSkill(t *testing.T) {
	p, f := rotationFixture()
	p.profile.Rotation.Steps[0] = rotationStep{ID: "finish-basic-chain", Action: "wait", Slot: 1, Requires: []string{"final_strike"}, Event: true, Attack: true}
	f.Signals["final_strike"] = false
	f.Signals["setup_applied"] = true
	if _, ok := p.next(f); ok {
		t.Fatal("skill dispatched before final strike")
	}
	if !p.shouldAttack(f) {
		t.Fatal("basic chain stopped while waiting for final strike")
	}
	f.Signals["final_strike"] = true
	p.next(f)
	if p.shouldAttack(f) {
		t.Fatal("held attack continued into next non-attack step")
	}
	if action, ok := p.next(f); !ok || action.ID != "burst" {
		t.Fatal("final strike did not unlock next ordered action")
	}
}
