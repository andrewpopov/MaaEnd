package innkeeping

import (
	"encoding/json"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
	"time"
)

var _ maa.CustomActionRunner = &Action{}

// Action reads one fresh inn screen and saves its next-action recommendation without input.
type Action struct{}

// Run keeps the live read bounded; the planner never assumes unseen dish icons or navigation.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return false
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return false
	}
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return false
	}
	tokens, err := screenocr.Read(ctx, img)
	if err != nil {
		log.Error().Err(err).Msg("inn screen OCR failed")
		return false
	}
	observation := Observe(tokens)
	state := State{Observation: observation}
	decision := Plan(state)
	report := struct {
		CapturedAt     string      `json:"captured_at"`
		Observation    Observation `json:"observation"`
		Decision       Decision    `json:"decision"`
		GameInputsSent bool        `json:"game_inputs_sent"`
	}{time.Now().UTC().Format(time.RFC3339), observation, decision, false}
	data, err := json.MarshalIndent(report, "", "    ")
	if err != nil {
		return false
	}
	folder := fsutil.OutputPath("data", "Innkeeping")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return false
	}
	temporary, err := os.CreateTemp(folder, "decision-*.tmp")
	if err != nil {
		return false
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return false
	}
	if err := temporary.Close(); err != nil {
		return false
	}
	// Windows cannot atomically replace an existing file with os.Rename.
	// Keep unique timestamped reports rather than overwrite another task's result.
	target := filepath.Join(folder, filepath.Base(name)+".json")
	if err := os.Rename(name, target); err != nil {
		return false
	}
	maafocus.Print(ctx, decision.Reason)
	if decision.BlockedBy != "" {
		maafocus.Print(ctx, decision.BlockedBy)
	}
	log.Info().Str("component", "InnkeepingAssistAction").Str("phase", observation.Phase).Str("decision", decision.Kind).Str("report", target).Msg("inn recommendation saved; no inputs sent")
	return observation.Phase != "unknown"
}
