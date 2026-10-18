// Package accountscan saves current-page evidence for the account scanning queue.
package accountscan

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
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &Action{}

// Action captures the current page without navigating or changing equipment.
type Action struct{}

type captureParam struct {
	TalentCount int    `json:"talent_count"`
	TaskID      string `json:"task_id"`
	Page        string `json:"page,omitempty"`
}

func validTask(value string) bool {
	switch value {
	case "account_identity", "operators", "skills", "equipped_weapons", "weapon_inventory", "equipped_gear", "gear_inventory", "gear_plans", "gear_recipes", "craft_materials", "artificing", "team_presets":
		return true
	default:
		return false
	}
}

func pythonPath() (string, error) {
	for _, candidate := range []string{
		fsutil.OutputPath("..", ".venv", "Scripts", "python.exe"),
		fsutil.OutputPath("..", ".venv", "bin", "python"),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("account scan Python runtime not found")
}

// Run saves evidence and imports it into the same SQLite store as Scan-Account.ps1.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var p captureParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil || !validTask(p.TaskID) {
		log.Error().Str("component", "AccountScanCaptureAction").Msg("invalid scan task")
		return false
	}
	if coreTask(p.TaskID) && p.Page == "" && !hasBatch(arg.TaskID) && pienv.ControllerKind() == pienv.ControllerKindAdb {
		return nextNode(ctx, arg.CurrentTaskName, "AccountBatchEntry"+p.TaskID)
	}
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
		log.Error().Err(err).Str("component", "AccountScanCaptureAction").Msg("missing 1280x720 capture")
		return false
	}
	inbox := fsutil.OutputPath("data", "AccountScans", "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		log.Error().Err(err).Msg("create scan inbox failed")
		return false
	}
	file, err := os.CreateTemp(inbox, p.TaskID+"-*.png")
	if err != nil {
		log.Error().Err(err).Msg("create scan evidence failed")
		return false
	}
	encodeErr := png.Encode(file, img)
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		log.Error().Err(encodeErr).AnErr("close_error", closeErr).Msg("write scan evidence failed")
		return false
	}
	tokens, err := screenocr.Read(ctx, img)
	if err != nil {
		log.Error().Err(err).Msg("account page OCR failed; screenshot retained")
		maafocus.Print(ctx, "Account page OCR failed; no account data was extracted.")
		return false
	}
	if p.TaskID == "operators" && p.Page == "overview" {
		if levelTokens, levelErr := screenocr.ReadRegion(ctx, img, maa.Rect{945, 110, 117, 67}, true); levelErr == nil {
			tokens = append(tokens, levelTokens...)
		}
	}
	if p.Page == "overview" {
		if nameTokens, nameErr := screenocr.ReadRegion(ctx, img, maa.Rect{170, 110, 350, 65}, false); nameErr == nil {
			tokens = append(tokens, nameTokens...)
		}
	}
	entity, uid, page, err := batchCaptureContext(arg.TaskID, p.Page, tokens)
	if err != nil {
		maafocus.Print(ctx, err.Error())
		return false
	}
	ocrPath := file.Name() + ".ocr.json"
	ocrData, err := json.Marshal(tokens)
	if err != nil {
		return false
	}
	if err := os.WriteFile(ocrPath, ocrData, 0o600); err != nil {
		return false
	}
	python, err := pythonPath()
	if err != nil {
		log.Error().Err(err).Str("evidence", file.Name()).Msg("scan runtime unavailable; PNG retained")
		return false
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, python, fsutil.OutputPath("tools", "account_scan.py"),
		"--store", fsutil.OutputPath("data", "AccountScans"), "ui-capture", "--task", p.TaskID, "--input", file.Name(), "--ocr", ocrPath)
	if entity != "" {
		command.Args = append(command.Args, "--entity", entity, "--uid", uid, "--page", page)
	}
	if p.TalentCount > 0 {
		command.Args = append(command.Args, "--talent-count", fmt.Sprint(p.TalentCount))
	}
	configureCommand(command)
	command.Dir = filepath.Dir(fsutil.OutputPath("interface.json"))
	output, err := command.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("evidence", file.Name()).Str("detail", string(output)).Msg("import scan evidence failed")
		return false
	}
	var result struct {
		Accepted  bool   `json:"accepted"`
		Reason    string `json:"reason"`
		TextCount int    `json:"ocr_text_count"`
		Report    string `json:"ocr_report"`
		Session   string `json:"session_id"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return false
	}
	if !result.Accepted {
		log.Warn().Str("scan_task", p.TaskID).Str("reason", result.Reason).Msg("account scan page rejected")
		maafocus.Print(ctx, result.Reason)
		return false
	}
	log.Info().Str("component", "AccountScanCaptureAction").Str("scan_task", p.TaskID).Int("ocr_text_count", result.TextCount).Msg("unreviewed account page text saved")
	if entity == "" {
		maafocus.Print(ctx, fmt.Sprintf("Saved %d OCR text regions for review. This task reads the open page; it does not scan other pages.", result.TextCount))
	} else {
		maafocus.Print(ctx, fmt.Sprintf("%s: saved %s (%d text regions).", entity, page, result.TextCount))
	}
	if entity != "" {
		recordBatchSession(arg.TaskID, result.Session)
	}
	if result.Report != "" && (entity == "" || page == "overview") {
		maafocus.Print(ctx, "OCR text: "+result.Report)
	}
	return true
}
