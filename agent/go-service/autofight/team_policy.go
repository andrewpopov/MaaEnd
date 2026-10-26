package autofight

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"regexp"
	"slices"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type teamRule struct {
	ID            string   `json:"id"`
	Action        string   `json:"action"`
	Slot          int      `json:"slot"`
	Requires      []string `json:"requires"`
	MinimumEnergy int      `json:"minimum_energy_bars"`
}

type teamProfile struct {
	Rotation         *guideRotation    `json:"rotation,omitempty"`
	IdentitySignal   string            `json:"identity_signal"`
	ID               string            `json:"id"`
	Members          []string          `json:"members"`
	DriverSlot       int               `json:"driver_slot"`
	RequiredSignals  []string          `json:"required_signals"`
	Signals          map[string]string `json:"signals"`
	Rules            []teamRule        `json:"rules"`
	ExclusiveSignals [][]string        `json:"exclusive_signals"`
}

type teamFrame struct {
	Preview     bool
	Now         time.Time
	ComboActive bool
	ComboSlots  []int
	Valid       bool
	Allowed     map[string]bool
	Selected    int
	Energy      int
	Enemy       bool
	Healthy     []int
	Signals     map[string]bool
}

type teamPolicy struct {
	rotation rotationState
	profile  teamProfile
	fired    map[string]bool
}

func validateTeamProfile(p teamProfile) error {
	if len(p.Members) != 4 || p.DriverSlot < 1 || p.DriverSlot > 4 || (len(p.Rules) == 0 && p.Rotation == nil) {
		return fmt.Errorf("team needs four operators, a driver, and rules")
	}
	names := map[string]bool{}
	for _, name := range p.Members {
		if name == "" || names[name] {
			return fmt.Errorf("operator names must be unique")
		}
		names[name] = true
	}
	for _, signal := range p.RequiredSignals {
		if p.Signals[signal] == "" {
			return fmt.Errorf("HUD calibration missing: %s", signal)
		}
	}
	if err := validateGuideRotation(p); err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, r := range p.Rules {
		if r.ID == "" || ids[r.ID] || r.Slot < 1 || r.Slot > 4 || r.MinimumEnergy < 0 || r.MinimumEnergy > 4 || len(r.Requires) == 0 {
			return fmt.Errorf("invalid combat rule %q", r.ID)
		}
		ids[r.ID] = true
		if !slices.Contains([]string{"skill", "ultimate", "combo", "switch"}, r.Action) {
			return fmt.Errorf("unsupported combat action %q", r.Action)
		}
		for _, signal := range r.Requires {
			if !slices.Contains(p.RequiredSignals, signal) && !slices.Contains([]string{"ultimate_1_ready", "ultimate_2_ready", "ultimate_3_ready", "ultimate_4_ready"}, signal) {
				return fmt.Errorf("undeclared combat signal %q", signal)
			}
		}
	}
	return nil
}

// next returns one observed, guarded action. An action is not retried until its condition changes.
func (p *teamPolicy) next(f teamFrame) (teamRule, bool) {
	if !f.Valid || !f.Enemy || f.Selected < 1 || f.Selected > 4 || !slices.Contains(f.Healthy, f.Selected) {
		return teamRule{}, false
	}
	if p.profile.IdentitySignal != "" && !f.Signals[p.profile.IdentitySignal] {
		return teamRule{}, false
	}
	for _, group := range p.profile.ExclusiveSignals {
		count := 0
		for _, signal := range group {
			if f.Signals[signal] {
				count++
			}
		}
		if count != 1 {
			return teamRule{}, false
		}
	}
	if p.profile.Rotation != nil {
		return p.nextRotation(f)
	}
	for _, r := range p.profile.Rules {
		ready := f.Energy >= r.MinimumEnergy
		if f.Allowed != nil {
			ready = ready && f.Allowed[r.Action]
		}
		for _, key := range r.Requires {
			ready = ready && f.Signals[key]
		}
		if r.Action == "switch" {
			ready = ready && f.Selected != r.Slot && slices.Contains(f.Healthy, r.Slot)
		}
		if !ready {
			delete(p.fired, r.ID)
			continue
		}
		if !p.fired[r.ID] {
			p.fired[r.ID] = true
			return r, true
		}
	}
	return teamRule{}, false
}

func loadTeamPolicy(id string, preview ...bool) (*teamPolicy, error) {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`).MatchString(id) {
		return nil, fmt.Errorf("invalid combat profile ID")
	}
	raw, err := os.ReadFile(fsutil.OutputPath("data", "CombatTeams", id+".json"))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1024*1024 {
		return nil, fmt.Errorf("combat profile exceeds size limit")
	}
	var profile teamProfile
	if err = json.Unmarshal(raw, &profile); err != nil {
		return nil, err
	}
	if profile.ID != id {
		return nil, fmt.Errorf("combat profile identity differs")
	}
	if err = validateTeamProfile(profile); err != nil {
		return nil, err
	}
	if len(preview) > 0 && preview[0] {
		return &teamPolicy{profile: profile, fired: map[string]bool{}}, nil
	}
	proof, err := os.ReadFile(fsutil.OutputPath("data", "CombatTeams", id+"-readiness.json"))
	if err != nil {
		return nil, fmt.Errorf("run team preflight: %w", err)
	}
	var readiness struct {
		Ready   bool   `json:"combat_ready"`
		Hash    string `json:"profile_sha256"`
		Created string `json:"created_at"`
	}
	if err = json.Unmarshal(proof, &readiness); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	created, err := time.Parse(time.RFC3339Nano, readiness.Created)
	age := time.Since(created)
	if err != nil || age < 0 || age > time.Hour || !readiness.Ready || readiness.Hash != hex.EncodeToString(sum[:]) {
		return nil, fmt.Errorf("team equipment/formation readiness is missing, stale, or blocked")
	}
	return &teamPolicy{profile: profile, fired: map[string]bool{}}, nil
}

func (p *teamPolicy) observe(ctx *maa.Context, img image.Image, selected, energy int, enemy bool, healthy, ultimates []int) teamFrame {
	f := teamFrame{Now: time.Now(), Valid: true, Selected: selected, Energy: energy, Enemy: enemy, Healthy: healthy, Signals: map[string]bool{}}
	for _, signal := range p.rotationSignals() {
		if slices.Contains([]string{"ultimate_1_ready", "ultimate_2_ready", "ultimate_3_ready", "ultimate_4_ready"}, signal) {
			continue
		}
		detail, err := ctx.RunRecognition(p.profile.Signals[signal], img)
		if err != nil || detail == nil {
			f.Valid = false
		}
		f.Signals[signal] = err == nil && detail != nil && detail.Hit
	}
	for slot := 1; slot <= 4; slot++ {
		f.Signals[fmt.Sprintf("ultimate_%d_ready", slot)] = slices.Contains(ultimates, slot)
	}
	return f
}

func teamAction(rule teamRule) ActionType {
	switch rule.Action {
	case "skill":
		return skillAction(rule.Slot)
	case "ultimate":
		return endSkillAction(rule.Slot)
	case "switch":
		return switchCharacterAction(rule.Slot)
	default:
		return ActionCombo
	}
}

func (p *teamPolicy) shouldAttack(f teamFrame) bool {
	allow := true
	if p.profile.Rotation != nil {
		allow = p.rotation.blocked == "" && p.profile.Rotation.Steps[p.rotation.cursor].Attack
	}
	return allow && f.Valid && f.Enemy && f.Selected == p.profile.DriverSlot && slices.Contains(f.Healthy, f.Selected) && (p.profile.IdentitySignal == "" || f.Signals[p.profile.IdentitySignal])
}
