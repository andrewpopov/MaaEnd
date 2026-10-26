package autofight

import (
	"fmt"
	"slices"
	"time"
)

// Timeline acknowledgement observes resource consumption, not damage landing.
// Only one input is outstanding so an SP drop cannot acknowledge a second cast.
type timelineExecution struct {
	pending       bool
	kind          string
	slot          int
	energy        int
	issued        time.Time
	lastFrame     time.Time
	confirmations int
	blocked       string
}

type timelineObservation struct {
	now           time.Time
	hudVisible    bool
	energy        int
	ultimates     []int
	comboReady    []int
	comboCooldown []int
}

func (e *timelineExecution) begin(kind string, slot, energy int, now time.Time) {
	e.pending = true
	e.kind = kind
	e.slot = slot
	e.energy = energy
	e.issued = now
	e.lastFrame = now
	e.confirmations = 0
}

func (e *timelineExecution) observe(f timelineObservation) bool {
	if !e.pending || e.blocked != "" || !f.now.After(e.lastFrame) {
		return false
	}
	e.lastFrame = f.now
	if f.now.Sub(e.issued) > 8*time.Second {
		e.blocked = fmt.Sprintf("No resource/cooldown acknowledgement for %s slot %d", e.kind, e.slot)
		return false
	}
	if !f.hudVisible {
		e.confirmations = 0
		return false
	}
	consumed := false
	switch e.kind {
	case "ultimate":
		consumed = !slices.Contains(f.ultimates, e.slot)
	case "skill":
		consumed = f.energy >= 0 && f.energy < e.energy
	case "combo":
		consumed = slices.Contains(f.comboCooldown, e.slot) && !slices.Contains(f.comboReady, e.slot)
	}
	if consumed {
		e.confirmations++
	} else {
		e.confirmations = 0
	}
	if e.confirmations < 2 {
		return false
	}
	e.pending = false
	return true
}

// These observations deliberately bypass multi-frame availability smoothing.
func (sa *ScreenAnalyzer) timelineObservation(now time.Time) timelineObservation {
	f := timelineObservation{now: now, energy: -1}
	if len(sa.frames) == 0 {
		return f
	}
	f.hudVisible = sa.hasLabelInFrames(LabelCharacterSelect, 1, false)
	for _, region := range energyRegions {
		if sa.hasLabelInFrames(LabelEnergyLevelFull, 1, false, region) {
			if f.energy < 0 {
				f.energy = 0
			}
			f.energy++
		}
	}
	if f.energy < 0 && sa.hasLabelInFrames(LabelEnergyLevelEmpty, 1, false, energyRegions[0]) {
		f.energy = 0
	}
	label := LabelEndSkillFull
	if sa.mobileLayout {
		label = LabelEndSkillFullADB
	}
	for i := 0; i < 4; i++ {
		portrait, ultimate := winCharacterRegions[i], winEndSkillRegions[i]
		if sa.mobileLayout {
			portrait, ultimate = adbCharacterRegions[i], adbEndSkillRegions[i]
		}
		if sa.hasLabelInFrames(label, 1, false, ultimate) {
			f.ultimates = append(f.ultimates, i+1)
		}
		if sa.hasLabelInFrames(LabelCharacterComboFull, 1, false, portrait) {
			f.comboReady = append(f.comboReady, i+1)
		}
		if sa.hasLabelInFrames(LabelCharacterComboEmpty, 1, false, portrait) {
			f.comboCooldown = append(f.comboCooldown, i+1)
		}
	}
	return f
}
