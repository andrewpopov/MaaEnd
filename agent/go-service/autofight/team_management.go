package autofight

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// TeamManagementAction prepares recommendations or upgrade costs. Farming is
// dispatched by the existing SupplyPlan pipeline after the planner validates it.
type TeamManagementAction struct{}

var _ maa.CustomActionRunner = &TeamManagementAction{}

func (a *TeamManagementAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	if _, err := fsutil.StartupDir(); err != nil {
		return false
	}
	param := struct {
		Mode          string `json:"mode"`
		TeamID        string `json:"team_id"`
		OperatorLevel int    `json:"operator_level"`
		WeaponLevel   int    `json:"weapon_level"`
		SkillRank     int    `json:"skill_rank"`
		MasteryRank   int    `json:"mastery_rank"`
	}{Mode: "recommend", TeamID: "moba-si-dps", OperatorLevel: 90, WeaponLevel: 90, SkillRank: 9}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		return false
	}
	node, err := ctx.GetNode("TeamManagementSettings")
	if err != nil {
		return false
	}
	attach, err := json.Marshal(node.Attach)
	if err != nil || json.Unmarshal(attach, &param) != nil {
		return false
	}
	if param.Mode == "capture" {
		return captureTeamUpgrade(ctx)
	}
	if param.Mode != "recommend" && param.Mode != "plan" && param.Mode != "farm" {
		return false
	}
	python, err := teamPlannerPython()
	if err != nil {
		log.Error().Err(err).Msg("team management Python runtime unavailable")
		return false
	}
	timeout, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(timeout, python, fsutil.OutputPath("tools", "team_manager.py"),
		"--root", fsutil.OutputPath(), "--mode", param.Mode, "--team", param.TeamID,
		"--level", fmt.Sprint(param.OperatorLevel), "--weapon", fmt.Sprint(param.WeaponLevel),
		"--skill", fmt.Sprint(param.SkillRank), "--mastery", fmt.Sprint(param.MasteryRank))
	raw, err := command.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("output", string(raw)).Msg("team management planning failed")
		maafocus.Print(ctx, "Team management could not read a valid account or build request. Check data/TeamManagement and the task log.")
		return false
	}
	var result struct {
		Report           string         `json:"report"`
		FarmReady        bool           `json:"farm_ready"`
		Blockers         int            `json:"blockers"`
		PipelineOverride map[string]any `json:"pipeline_override"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return false
	}
	maafocus.Print(ctx, fmt.Sprintf("Team management: %d checks outstanding. Report: %s", result.Blockers, result.Report))
	if param.Mode != "farm" {
		return true
	}
	if !result.FarmReady || len(result.PipelineOverride) == 0 {
		maafocus.Print(ctx, "Farming was not started. Review the upgrade costs, current levels, evidence and supported material sources in the report.")
		return false
	}
	// The pipeline owns all navigation and farming. Never run a second tasker here.
	if err = ctx.ClearHitCount("SyncItemData"); err != nil {
		return false
	}
	if err = ctx.ClearHitCount("SyncItemDataScrollPage"); err != nil {
		return false
	}
	result.PipelineOverride[arg.CurrentTaskName] = map[string]any{"next": []string{"SupplyPlanMain"}}
	if err = ctx.OverridePipeline(result.PipelineOverride); err != nil {
		log.Error().Err(err).Msg("team material farming override failed")
		return false
	}
	return true
}

// captureTeamUpgrade records the current upgrade or gear-plan preview. Evidence
// stays unreviewed and is never converted into approved spending automatically.
func captureTeamUpgrade(ctx *maa.Context) bool {
	tasker := ctx.GetTasker()
	if tasker == nil {
		return false
	}
	controller := tasker.GetController()
	if controller == nil {
		return false
	}
	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil || img == nil || img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 720 {
		return false
	}
	dir := fsutil.OutputPath("data", "TeamManagement", "captures")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	file, err := os.CreateTemp(dir, "upgrade-*.png")
	if err != nil {
		return false
	}
	err = png.Encode(file, img)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return false
	}
	tokens, err := screenocr.Read(ctx, img)
	if err != nil {
		return false
	}
	record := map[string]any{"observed_at": time.Now().UTC().Format(time.RFC3339Nano),
		"image": filepath.Base(file.Name()), "reviewed": false, "game_inputs_sent": false, "ocr": tokens}
	raw, err := json.MarshalIndent(record, "", "    ")
	if err != nil || fsutil.WriteFileAtomic(file.Name()+".json", raw, 0o600) != nil {
		return false
	}
	maafocus.Print(ctx, fmt.Sprintf("Saved upgrade/gear-plan evidence: %s. Review exact levels, item identities, quantities and account UID before using its costs.", file.Name()))
	return true
}
