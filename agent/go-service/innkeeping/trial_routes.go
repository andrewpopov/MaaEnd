package innkeeping

import (
	"encoding/json"
	"fmt"
	"image"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const trialRouteProfileID = "front-row-maatouch-v1"

type trialRouteProfile struct {
	ID         string       `json:"id"`
	PickupNear scenePatch   `json:"pickup_near"`
	Routes     []fixedRoute `json:"routes"`
}

// trialSelectionNode proves the native Trial button was acknowledged in this
// task, rather than accepting an externally asserted mode or an old screenshot.
func trialSelectionNode(n *maa.NodeDetail) bool {
	if n == nil || n.Name != "InnCalTheme" || !n.RunCompleted || n.Action == nil || !n.Action.Success || n.Action.Action != "LongPress" || n.Recognition == nil || !n.Recognition.Hit {
		return false
	}
	b := n.Action.Box
	x, y := b[0]+b[2]/2, b[1]+b[3]/2
	return x >= 700 && x <= 990 && y >= 600 && y <= 690
}

func loadTrialRouteProfile(t *maa.Tasker, taskID int64, id string) (*trialRouteProfile, error) {
	if id != trialRouteProfileID {
		return nil, fmt.Errorf("unknown Trial route profile")
	}
	detail, err := t.GetTaskDetail(taskID)
	if err != nil {
		return nil, err
	}
	trial := false
	for _, ref := range detail.Nodes {
		n, e := ref.GetDetail()
		if e == nil && trialSelectionNode(n) {
			trial = true
			break
		}
	}
	if !trial {
		return nil, fmt.Errorf("this task has no acknowledged Trial selection")
	}
	raw, err := os.ReadFile(fsutil.OutputPath("data", "Innkeeping", "trial-routes.json"))
	if err != nil {
		return nil, err
	}
	var profile trialRouteProfile
	if err = json.Unmarshal(raw, &profile); err != nil {
		return nil, err
	}
	if profile.ID != id || len(profile.Routes) == 0 || !scenePatchSetMatches([]scenePatch{profile.PickupNear}, []scenePatch{profile.PickupNear}) {
		return nil, fmt.Errorf("invalid Trial pickup geometry")
	}
	for _, r := range profile.Routes {
		if !validFixedRoute(r) || r.Seat != "1" || !trialMiddleSteps(r) {
			return nil, fmt.Errorf("invalid measured Trial leg %s", r.ID)
		}
	}
	return &profile, nil
}

// trialMiddleSteps admits only the measured first-table middle-lane sequence.
// Other tables and loaded returns require their own actual validation.
func trialMiddleSteps(r fixedRoute) bool {
	expected := [][3]int{{115, 551, 600}, {195, 471, 700}, {275, 551, 450}}
	if r.Goal == "counter" {
		expected = [][3]int{{115, 551, 450}, {195, 631, 700}, {275, 551, 600}}
	}
	if len(r.Steps) != len(expected) {
		return false
	}
	for i, step := range r.Steps {
		e := expected[i]
		if step.Kind != "walk" || !step.Pipeline || step.X != e[0] || step.Y != e[1] || step.Milliseconds != 20 || step.HoldMilliseconds != e[2] {
			return false
		}
	}
	return true
}

func (s *liveSession) startTrialRoute(img image.Image, v LiveView) bool {
	if s.TrialRoutes == nil || len(v.Unknown) != 0 {
		return false
	}
	held := trayCount(v.Tray)
	if held > 1 {
		return false
	}
	goal := "counter"
	if held == 1 {
		goal = "diner"
		near := s.TrialRoutes.PickupNear
		if !atPickupCounter(v) || !scenePatchSetMatches([]scenePatch{sampleScene(img, near.Box)}, []scenePatch{near}) {
			return false
		}
	}
	for _, r := range s.TrialRoutes.Routes {
		if r.Goal != goal || !routeDepartureNearby(img, r) {
			continue
		}
		if goal == "counter" && s.Fixed.Seat != "1" {
			continue
		}
		if goal == "diner" {
			compatible := false
			for _, seat := range v.Seats {
				if seat.Seat == "1" && includes(seat.Dishes, selectedTrayDish(v)) {
					compatible = true
				}
			}
			if !compatible {
				continue
			}
		}
		s.Fixed.Playback = &routePlayback{Route: r, Trial: true, TrayBefore: maps.Clone(v.Tray), Started: time.Now(), LastCompleted: time.Now()}
		s.Fixed.Recording = &routeRecording{fixedRoute: r}
		return true
	}
	return false
}

type trialGesture struct {
	Label   string            `json:"label"`
	At      time.Time         `json:"at"`
	Steps   []movementCommand `json:"steps"`
	Before  string            `json:"before"`
	After   string            `json:"after"`
	Status  string            `json:"status"`
	TaskIDs []int64           `json:"task_ids"`
}

func (s *liveSession) recordTrialBoundary(c *maa.Controller, label string) (string, error) {
	c.PostScreencap().Wait()
	img, err := c.CacheImage()
	if err != nil || img == nil {
		return "", fmt.Errorf("missing fresh Trial boundary image")
	}
	return s.captureEvidence(time.Now(), img, label)
}

func (s *liveSession) saveTrialGestures(p *routePlayback, taskID int64) error {
	path := filepath.Join(s.CaptureRoot, fmt.Sprintf("trial-task%d-gestures.jsonl", taskID))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, row := range p.TrialGestures {
		row.Status = "Succeeded"
		if err = json.NewEncoder(f).Encode(row); err != nil {
			return err
		}
	}
	return nil
}
