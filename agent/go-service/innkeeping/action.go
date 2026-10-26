package innkeeping

import (
	"encoding/json"
	"fmt"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var _ maa.CustomActionRunner = &Action{}

// Action reads one fresh inn screen and saves its next-action recommendation without input.
type Action struct {
	once    sync.Once
	session *liveSession
	err     error
}

// Run keeps the live read bounded; the planner never assumes unseen dish icons or navigation.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	a.once.Do(func() { a.session, a.err = newLiveSession() })
	if a.err != nil {
		log.Error().Err(a.err).Msg("Load inn read-only capture data")
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
	stage := time.Now()
	timings := map[string]float64{}
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return false
	}
	timings["capture"] = time.Since(stage).Seconds()
	stage = time.Now()
	var options struct {
		Focus string `json:"focus"`
	}
	if arg.CustomActionParam != "" && json.Unmarshal([]byte(arg.CustomActionParam), &options) != nil {
		return false
	}
	var tokens []screenocr.Token
	if options.Focus == "kitchen" {
		// Warm check-in: queue/tray/order icons use the same frame; OCR only
		// reads kitchen identity and its active countdown. Recipes are cached
		// by the caller after a full read and refreshed on any discrepancy.
		tokens, err = screenocr.ReadRegion(ctx, img, maa.Rect{40, 95, 600, 65}, false)
		if err != nil || Observe(tokens).Phase != "kitchen" {
			return false
		}
		timer, readErr := screenocr.ReadRegion(ctx, img, maa.Rect{310, 600, 80, 40}, false)
		if readErr != nil {
			return false
		}
		tokens = append(tokens, timer...)
	} else if options.Focus == "service" {
		// Delivery needs the guest HUD, phase/timer, physical plaques and
		// interaction text. Counter-food search and recipe OCR cannot affect it.
		for _, region := range []maa.Rect{{0, 0, 1100, 145}, {720, 390, 480, 80}} {
			part, readErr := screenocr.ReadRegion(ctx, img, region, false)
			if readErr != nil {
				return false
			}
			tokens = append(tokens, part...)
		}
		if Observe(tokens).Phase == "unknown" {
			// Results and unexpected menus must retain full-screen recognition.
			tokens, err = screenocr.Read(ctx, img)
			if err != nil {
				return false
			}
		}
	} else if options.Focus == "" {
		tokens, err = screenocr.Read(ctx, img)
		if err != nil {
			log.Error().Err(err).Msg("inn screen OCR failed")
			return false
		}
		cropped, readErr := screenocr.ReadRegion(ctx, img, maa.Rect{80, 280, 1200, 180}, false)
		if readErr != nil {
			return false
		}
		tokens = append(tokens, cropped...)
	} else {
		return false
	}
	timings["ocr"] = time.Since(stage).Seconds()
	stage = time.Now()
	capturedAt := time.Now()
	imagePath, err := a.session.captureEvidence(capturedAt, img, "native-observation")
	if err != nil {
		log.Error().Err(err).Msg("Save native inn observation")
		return false
	}
	timings["save_image"] = time.Since(stage).Seconds()
	stage = time.Now()
	view := ReadLive(img, tokens, a.session.Icons, a.session.Catalog)
	view = focusedLiveTimes(view, a.session.Catalog, func(region maa.Rect) ([]screenocr.Token, error) {
		extra, err := screenocr.ReadRegion(ctx, img, region, true)
		tokens = append(tokens, extra...)
		return extra, err
	})
	timings["hud_and_timers"] = time.Since(stage).Seconds()
	stage = time.Now()
	var counterFoods []WorldFood
	if options.Focus != "service" && (view.Phase == "active" || view.Phase == "closing" || view.Phase == "closed_serving") {
		counterFoods = WorldFoods(img, a.session.Icons, true)
	}
	timings["counter_foods"] = time.Since(stage).Seconds()
	observation := Observe(tokens)
	state := State{Observation: observation}
	decision := Plan(state)
	report := struct {
		Timings        map[string]float64 `json:"timings_seconds"`
		CapturedAt     string             `json:"captured_at"`
		TaskID         int64              `json:"task_id"`
		Image          string             `json:"image"`
		Tokens         []screenocr.Token  `json:"tokens"`
		View           LiveView           `json:"view"`
		CounterFoods   []WorldFood        `json:"counter_foods"`
		Observation    Observation        `json:"observation"`
		Decision       Decision           `json:"decision"`
		GameInputsSent bool               `json:"game_inputs_sent"`
	}{Timings: timings, CapturedAt: capturedAt.UTC().Format(time.RFC3339Nano), TaskID: arg.TaskID, Image: imagePath,
		Tokens: tokens, View: view, CounterFoods: counterFoods, Observation: observation, Decision: decision}
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
	target := filepath.Join(folder, fmt.Sprintf("decision-%d-%s.json", arg.TaskID, filepath.Base(name)))
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
