package innkeeping

import (
	"encoding/json"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"image"
	"os"
	"path/filepath"
	"sync"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// CounterOriginRecognition supplies fresh visual feedback to bounded Pipeline
// corrections. Nearby matches never authorize departure; only the strict mode does.
type CounterOriginRecognition struct {
	mu           sync.Mutex
	capturedTask int64
}

type counterOriginParams struct {
	Reference      sceneFingerprint `json:"reference"`
	Mode           string           `json:"mode"`
	CaptureFailure bool             `json:"capture_failure,omitempty"`
}

func counterCorrectionMode(offsets [2][2]int) string {
	a, b := offsets[0], offsets[1]
	// Matching upper landmarks move upward together when the character stops
	// short of the bar. Lateral-only steering cannot remove that depth error.
	if a[1] <= -6 && b[1] <= -6 && a[1] >= -12 && b[1] >= -12 && abs(a[1]-b[1]) <= 4 && abs(a[0]-b[0]) <= 24 {
		return "backward"
	}
	if abs(a[0]-b[0]) > 12 || abs(a[1]) > 12 || abs(b[1]) > 12 {
		return ""
	}
	// Both landmarks can straddle zero while still needing a tiny correction.
	// Their six-pixel acceptance intervals must overlap; the midpoint steers
	// toward that intersection without relaxing the actual departure gate.
	if abs(a[0]) <= 6 && abs(b[0]) <= 6 {
		return ""
	}
	center := (a[0] + b[0]) / 2
	distance := abs(center)
	if distance < 1 || distance > 78 {
		return ""
	}
	side := "right"
	if center < 0 {
		side = "left"
	}
	if distance >= 40 {
		return side + "_large"
	}
	if distance >= 20 {
		return side + "_coarse"
	}
	return side + "_fine"
}

func counterOriginMatch(img image.Image, p counterOriginParams) ([2][2]int, bool) {
	var offsets [2][2]int
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) ||
		!scenePatchSetMatches(p.Reference[:], p.Reference[:]) {
		return offsets, false
	}
	if p.Mode == "aligned" {
		return locateRouteSceneWindow(img, p.Reference, 6, 4)
	}
	if sceneMatchesImage(img, p.Reference) {
		return offsets, false
	}
	offsets, ok := locateRouteSceneWindow(img, p.Reference, 78, 12)
	return offsets, ok && counterCorrectionMode(offsets) == p.Mode
}

func (r *CounterOriginRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		return nil, false
	}
	var p counterOriginParams
	if json.Unmarshal([]byte(arg.CustomRecognitionParam), &p) != nil {
		return nil, false
	}
	offsets, ok := counterOriginMatch(arg.Img, p)
	if !ok {
		if p.CaptureFailure && p.Mode == "aligned" && arg.Img != nil {
			r.mu.Lock()
			if r.capturedTask != arg.TaskID {
				r.capturedTask = arg.TaskID
				var layout struct {
					CaptureRoot string `json:"capture_root"`
				}
				data, err := os.ReadFile(fsutil.OutputPath("data", "Innkeeping", "reactive-layout.json"))
				if err == nil && json.Unmarshal(data, &layout) == nil && filepath.IsAbs(layout.CaptureRoot) {
					session := liveSession{CaptureRoot: layout.CaptureRoot}
					path, err := session.captureEvidence(time.Now(), arg.Img, "counter-alignment-failed")
					log.Warn().Err(err).Str("image", path).Int64("task", arg.TaskID).Msg("Counter alignment frame rejected")
				}
			}
			r.mu.Unlock()
		}
		return nil, false
	}
	log.Debug().Str("component", "InnkeepingCounterOrigin").Str("mode", p.Mode).Interface("offsets", offsets).Msg("Fresh counter alignment feedback")
	detail, _ := json.Marshal(map[string]any{"mode": p.Mode, "offsets": offsets})
	return &maa.CustomRecognitionResult{Box: maa.Rect{570, 150, 280, 40}, Detail: string(detail)}, true
}
