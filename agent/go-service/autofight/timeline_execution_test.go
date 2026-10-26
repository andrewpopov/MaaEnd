package autofight

import (
	"testing"
	"time"
)

func TestTimelineWaitsForResourceUse(t *testing.T) {
	for _, kind := range []string{"skill", "ultimate", "combo"} {
		t.Run(kind, func(t *testing.T) {
			start := time.Unix(100, 0)
			e := timelineExecution{}
			e.begin(kind, 2, 3, start)
			f := timelineObservation{now: start.Add(time.Second), hudVisible: true, energy: 3, ultimates: []int{2}, comboReady: []int{2}}
			if e.observe(f) {
				t.Fatal("unchanged HUD acknowledged cast")
			}
			f.energy = 2
			f.ultimates = nil
			f.comboReady = nil
			f.comboCooldown = []int{2}
			f.now = f.now.Add(time.Second)
			if e.observe(f) {
				t.Fatal("single frame confirmed cast")
			}
			if e.observe(f) {
				t.Fatal("duplicate frame confirmed cast")
			}
			f.now = f.now.Add(time.Second)
			if !e.observe(f) || e.pending {
				t.Fatal("two resource-use observations did not confirm")
			}
		})
	}
}
func TestTimelineUnknownHUDCannotConfirmUltimate(t *testing.T) {
	start := time.Unix(100, 0)
	e := timelineExecution{}
	e.begin("ultimate", 1, 3, start)
	for i := 1; i <= 3; i++ {
		if e.observe(timelineObservation{now: start.Add(time.Duration(i) * time.Second), energy: -1}) {
			t.Fatal("missing HUD confirmed ultimate")
		}
	}
}
func TestTimelineComboNeedsCooldownForIntendedOwner(t *testing.T) {
	start := time.Unix(100, 0)
	e := timelineExecution{}
	e.begin("combo", 2, 3, start)
	for i := 1; i <= 3; i++ {
		if e.observe(timelineObservation{now: start.Add(time.Duration(i) * time.Second), hudVisible: true, comboCooldown: []int{3}}) {
			t.Fatal("another operator cooldown confirmed combo")
		}
	}
}
func TestTimelineLostInputBlocksWithoutAdvancing(t *testing.T) {
	start := time.Unix(100, 0)
	e := timelineExecution{}
	e.begin("skill", 2, 3, start)
	if e.observe(timelineObservation{now: start.Add(9 * time.Second), hudVisible: true, energy: 3}) || e.blocked == "" || !e.pending {
		t.Fatal("lost input did not remain blocked")
	}
}
func TestEndAxisCollectsComboStepsInOrder(t *testing.T) {
	sc := &timelineScenarioRaw{Data: timelineDataRaw{Tracks: []timelineTrackRaw{
		{Actions: []timelineActionRaw{{Type: "battleSkill", StartTime: 300}, {Type: "comboSkill", StartTime: 400}, {Type: "attack", StartTime: 350}}},
		{Actions: []timelineActionRaw{{Type: "ultimate", StartTime: 500}}},
	}}}
	actions := collectTimelineActions(sc)
	if len(actions) != 3 || actions[0].Type != "skill" || actions[1].Type != "combo" || actions[2].Type != "ultimate" {
		t.Fatalf("wrong ordered actions: %+v", actions)
	}
}
