package innkeeping

import (
	"image"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// walkBurst retains only image samples and a single observed destination. Each
// extra gesture still needs a fresh target and an empty interaction prompt.
type walkBurst struct {
	HUD     *cameraSearch
	Dish    string
	Counter bool
	Last    [4]int
	Steps   int
	Pixels  [][][3]int
}

func walkHUDPixels(img image.Image) [][][3]int {
	var pixels [][][3]int
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) {
		return nil
	}
	// Order cards and the entire tray, including selection outlines. Exclude the
	// changing clock and the world marker; neither may authorize another step.
	var boxes []image.Rectangle
	for seat := 0; seat < 6; seat++ {
		x := 234 + 118*seat
		boxes = append(boxes, image.Rect(x, 40, x+116, 108))
	}
	for slot := 0; slot < 5; slot++ {
		x := 445 + 79*slot
		boxes = append(boxes, image.Rect(x, 589, x+75, 663))
	}
	for _, box := range boxes {
		var patch [][3]int
		for y := box.Min.Y; y < box.Max.Y; y += 3 {
			for x := box.Min.X; x < box.Max.X; x += 3 {
				r, g, b, _ := img.At(x, y).RGBA()
				patch = append(patch, [3]int{int(r >> 8), int(g >> 8), int(b >> 8)})
			}
		}
		pixels = append(pixels, patch)
	}
	return pixels
}

func newWalkBurst(img image.Image, v LiveView, cmd movementCommand, goals []WorldFood, counter bool, now time.Time) *walkBurst {
	if cmd.Kind != "walk" || cmd.Box[2] == 0 || cmd.Box[1]+cmd.Box[3] >= 300 || v.Prompt != "" {
		return nil
	}
	dish := ""
	for _, goal := range goals {
		if goal.Box == cmd.Box && goal.Counter == counter && goal.Dish == cmd.Dish {
			dish = goal.Dish
			break
		}
	}
	if dish == "" {
		return nil
	}
	hud := newCameraSearch(img, v.AddOrderBox, nil, counter, now)
	if hud == nil {
		return nil
	}
	return &walkBurst{HUD: hud, Dish: dish, Counter: counter, Last: cmd.Box, Pixels: walkHUDPixels(img)}
}

func (w *walkBurst) matches(img image.Image, now time.Time) bool {
	if w == nil || w.Steps >= 2 || !w.HUD.matches(img, now) {
		return false
	}
	pixels := walkHUDPixels(img)
	if len(pixels) == 0 || len(pixels) != len(w.Pixels) {
		return false
	}
	for i, patch := range pixels {
		if len(patch) != len(w.Pixels[i]) {
			return false
		}
		sum, changed := 0, 0
		for j, p := range patch {
			d := abs(p[0]-w.Pixels[i][j][0]) + abs(p[1]-w.Pixels[i][j][1]) + abs(p[2]-w.Pixels[i][j][2])
			sum += d
			if d > 60 {
				changed++
			}
		}
		if sum > len(patch)*30 || changed*100 > len(patch)*10 {
			return false
		}
	}
	return true
}

func (w *walkBurst) target(goals []WorldFood) (WorldFood, bool) {
	if w == nil {
		return WorldFood{}, false
	}
	var result WorldFood
	count := 0
	for _, goal := range goals {
		b := goal.Box
		if goal.Dish != w.Dish || goal.Counter != w.Counter || b[1]+b[3] >= 340 {
			continue
		}
		if abs(b[0]+b[2]/2-w.Last[0]-w.Last[2]/2) > 100 || abs(b[1]-w.Last[1]) > 80 || abs(b[2]-w.Last[2]) > 60 {
			continue
		}
		if count > 0 && result.Box == b {
			continue
		}
		result = goal
		count++
	}
	if count != 1 {
		return WorldFood{}, false
	}
	b := result.Box
	// A stationary marker needs ordinary navigation's obstacle assessment.
	progress := abs(b[0]-w.Last[0]) + abs(b[1]-w.Last[1]) + abs(b[2]-w.Last[2]) + abs(b[3]-w.Last[3])
	return result, progress >= 7
}

var _ maa.CustomActionRunner = &InnWalkBurstAction{}

// InnWalkBurstAction validates one short additional walk. Pipeline owns the
// bounded repetition and restores the full planner when any guard changes.
type InnWalkBurstAction struct{ owner *ReactiveAction }

func (a *InnWalkBurstAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
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
	w := s.Walk
	exit := func() bool { s.Walk = nil; return route(ctx, arg, "InnkeepingReactiveObserve") }
	if !w.matches(img, time.Now()) {
		return exit()
	}
	// This crop contains the actual Serve/Welcome/food/Bar labels, including
	// wrapped food names. Any text yields to full observation before movement.
	tokens, err := screenocr.ReadRegion(ctx, img, maa.Rect{800, 390, 400, 80}, false)
	if err != nil {
		return false
	}
	for _, token := range tokens {
		if token.Score >= .5 {
			return exit()
		}
	}
	goal, ok := w.target(WorldFoods(img, s.Icons, w.Counter))
	if !ok || !w.matches(img, time.Now()) {
		return exit()
	}
	cmd, reason := planMovement(&s.Navigation, time.Now(), []WorldFood{goal}, map[string]bool{w.Dish: true}, w.Counter)
	if reason != "" || cmd.Kind != "walk" {
		return exit()
	}
	cmd = navigationGesture(cmd)
	s.rememberStep(img, cmd)
	if !issueMovement(ctx, ctrl, cmd) {
		return false
	}
	w.Last = goal.Box
	w.Steps++
	log.Info().Str("component", "InnkeepingWalkBurstAction").Interface("command", cmd).Int("step", w.Steps).Msg("fresh target verified before fast walk")
	return route(ctx, arg, "InnkeepingWalkBurst")
}
