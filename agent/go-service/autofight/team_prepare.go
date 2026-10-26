package autofight

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &TeamCombatPrepareAction{}

// TeamCombatPrepareAction writes a read-only team gear and combat readiness report.
type TeamCombatPrepareAction struct{}

func teamPlannerPython() (string, error) {
	for _, path := range []string{fsutil.OutputPath("..", ".venv", "Scripts", "python.exe"), fsutil.OutputPath("..", ".venv", "bin", "python")} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("team planner Python runtime is unavailable")
}

func selectedCombatProfile(ctx *maa.Context) (string, error) {
	node, err := ctx.GetNode("TeamCombatSettings")
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(node.Attach)
	if err != nil {
		return "", err
	}
	setting := struct {
		Profile string `json:"profile_id"`
	}{Profile: "heat-si"}
	if err = json.Unmarshal(raw, &setting); err != nil {
		return "", err
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`).MatchString(setting.Profile) {
		return "", fmt.Errorf("invalid combat profile")
	}
	return setting.Profile, nil
}

// Run creates the report without navigating, equipping, crafting, or spending.
func (a *TeamCombatPrepareAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	if _, err := fsutil.StartupDir(); err != nil {
		return false
	}
	python, err := teamPlannerPython()
	if err != nil {
		log.Error().Err(err).Msg("team planner unavailable")
		return false
	}
	timeout, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	profileID, err := selectedCombatProfile(ctx)
	if err != nil {
		return false
	}
	output := fsutil.OutputPath("data", "CombatTeams", profileID+"-readiness.json")
	command := exec.CommandContext(timeout, python, fsutil.OutputPath("tools", "combat_team.py"), "--profile", fsutil.OutputPath("data", "CombatTeams", profileID+".json"), "--store", fsutil.OutputPath("data", "AccountScans"), "--output", output)
	raw, err := command.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("component", "TeamCombatPrepareAction").Msg("team preflight failed")
		maafocus.Print(ctx, "Team preparation could not read the account scan; complete Account and server capture first.")
		return false
	}
	var result struct {
		Status   string `json:"status"`
		Blockers int    `json:"blockers"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		log.Error().Err(err).Msg("invalid team preflight output")
		return false
	}
	maafocus.Print(ctx, fmt.Sprintf("%s: %s (%d readiness checks outstanding). Report: data/CombatTeams/%s-readiness.html", profileID, result.Status, result.Blockers, profileID))
	return true
}
