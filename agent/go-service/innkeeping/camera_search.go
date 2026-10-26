package innkeeping

import (
	"encoding/json"
	"image"
	"os"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// cameraSearch borrows no framework handles. Its short burst only rotates the
// camera; every walk, pickup and serve still returns to full fresh observation.
type cameraSearch struct {
	Started time.Time
	Pans    int
	Counter bool
	Wanted  map[string]bool
	Box     image.Rectangle
	Ink     []bool
}

func whiteHUDPixel(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return min(r, g, b) >= 200*257 && max(r, g, b)-min(r, g, b) < 40*257
}

func newCameraSearch(img image.Image, box [4]int, wanted map[string]bool, counter bool, now time.Time) *cameraSearch {
	r := image.Rect(box[0], box[1], box[0]+box[2], box[1]+box[3])
	// Only a freshly OCR-verified Add Order label may seed the world HUD guard.
	if img == nil || box[0] < 840 || box[1] < 600 || box[2] < 40 || box[3] < 10 || !r.In(img.Bounds()) {
		return nil
	}
	s := &cameraSearch{Started: now, Counter: counter, Wanted: map[string]bool{}, Box: r}
	for key, value := range wanted {
		s.Wanted[key] = value
	}
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			white := whiteHUDPixel(img, x, y)
			s.Ink = append(s.Ink, white)
			if white {
				n++
			}
		}
	}
	if n < 40 {
		return nil
	}
	return s
}

func (s *cameraSearch) matches(img image.Image, now time.Time) bool {
	if s == nil || img == nil || !s.Box.In(img.Bounds()) || now.Sub(s.Started) > 2*time.Second || s.Pans >= 4 {
		return false
	}
	index, reference, current, shared := 0, 0, 0, 0
	for y := s.Box.Min.Y; y < s.Box.Max.Y; y++ {
		for x := s.Box.Min.X; x < s.Box.Max.X; x++ {
			white := whiteHUDPixel(img, x, y)
			if s.Ink[index] {
				reference++
				if white {
					shared++
				}
			}
			if white {
				current++
			}
			index++
		}
	}
	return reference > 0 && current > 0 && shared*100 >= reference*85 && shared*100 >= current*85
}

var _ maa.CustomActionRunner = &InnCameraSearchAction{}

// InnCameraSearchAction checks fresh image geometry between bounded camera pans.
// Pipeline owns repetition; a changed HUD, target or expired burst restores OCR.
type InnCameraSearchAction struct{ owner *ReactiveAction }

func (a *InnCameraSearchAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || a.owner == nil {
		return false
	}
	a.owner.mu.Lock()
	defer a.owner.mu.Unlock()
	s := a.owner.sessions[arg.TaskID]
	if s == nil || !s.Automatic || !s.Started {
		return false
	}
	t := ctx.GetTasker()
	if t == nil {
		return false
	}
	ctrl := t.GetController()
	if ctrl == nil {
		return false
	}
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return false
	}
	search := s.Search
	if !search.matches(img, time.Now()) {
		s.Search = nil
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	goals := WorldFoods(img, s.Icons, search.Counter)
	for _, goal := range goals {
		if search.Wanted[goal.Dish] {
			s.Search = nil
			// Navigation itself performs a full fresh read before moving.
			return route(ctx, arg, "InnkeepingReactiveNavigate")
		}
	}
	cmd, reason := planMovement(&s.Navigation, time.Now(), goals, search.Wanted, search.Counter)
	if reason != "" || cmd.Kind != "camera" {
		s.Search = nil
		return route(ctx, arg, "InnkeepingReactiveObserve")
	}
	cmd = navigationGesture(cmd)
	if !search.Counter {
		// Preserve the actual searched views; a final frame cannot explain
		// which distant diner markers were clipped or rejected mid-scan.
		if err := s.evidence(time.Now(), img, "diner-search"); err != nil {
			return false
		}
	}
	s.rememberStep(img, cmd)
	if !issueMovement(ctx, ctrl, cmd) {
		return false
	}
	search.Pans++
	row, _ := json.Marshal(struct {
		At         time.Time       `json:"at"`
		Command    movementCommand `json:"command"`
		Candidates []WorldFood     `json:"candidates"`
		Fast       bool            `json:"fast_search"`
	}{time.Now(), cmd, goals, true})
	if f, err := os.OpenFile(fsutil.OutputPath("data", "Innkeeping", "navigation.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		_, _ = f.Write(append(row, '\n'))
		f.Close()
	}
	return route(ctx, arg, "InnkeepingCameraSearch")
}
