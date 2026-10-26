package autofight

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomActionRunner = &TeamCombatCaptureAction{}

// TeamCombatCaptureAction captures one fresh HUD and OCR result without combat inputs.
type TeamCombatCaptureAction struct{}

// Run retains unreviewed evidence; it never creates recognition bindings or arms a policy.
func (a *TeamCombatCaptureAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	if _, err := fsutil.StartupDir(); err != nil {
		return false
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
		return false
	}
	profileID, err := selectedCombatProfile(ctx)
	if err != nil {
		return false
	}
	dir := fsutil.OutputPath("data", "CombatTeams", "captures")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	file, err := os.CreateTemp(dir, "hud-*.png")
	if err != nil {
		return false
	}
	err = png.Encode(file, img)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return false
	}
	tokens, ocrErr := screenocr.Read(ctx, img)
	record := map[string]any{"captured_at": time.Now().UTC().Format(time.RFC3339Nano), "image": filepath.Base(file.Name()), "profile_id": profileID, "reviewed": false, "state_inferred": false, "game_inputs_sent": false, "ocr": tokens}
	if ocrErr != nil {
		record["ocr_error"] = ocrErr.Error()
	}
	raw, err := json.MarshalIndent(record, "", "    ")
	if err != nil {
		return false
	}
	if err = os.WriteFile(file.Name()+".json", raw, 0o600); err != nil {
		return false
	}
	maafocus.Print(ctx, fmt.Sprintf("Saved unreviewed team HUD + OCR: data/CombatTeams/captures/%s. No inputs sent; current team and Si phase still need review.", filepath.Base(file.Name())))
	return true
}
