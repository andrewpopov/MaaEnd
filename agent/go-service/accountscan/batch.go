package accountscan

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"image"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var _ maa.CustomActionRunner = &BatchAction{}

// BatchAction performs bounded bookkeeping and image-based list/scroll decisions.
// Every click and page transition remains in the Pipeline.
type BatchAction struct{}
type batchParam struct {
	Points [][2]int `json:"points"`
	Resume bool     `json:"resume"`
	Step   string   `json:"step"`
	Task   string   `json:"task_id"`
	Page   string   `json:"page"`
	Next   string   `json:"next"`
}
type batchRun struct {
	Mode, Name, UID, Session             string
	Prepared, Resume                     bool
	Seen                                 []string
	GridBefore, RailBefore, ScrollBefore []float64
	GridProbes, RailProbes, ScrollProbes int
	ScrollPage, ScrollNext               string
}

var batchMu sync.Mutex
var batchRuns = map[int64]*batchRun{}
var uidPattern = regexp.MustCompile(`(?i)UID\s*[:\x{ff1a}]?\s*(\d{8,12})`)

func coreTask(s string) bool {
	return s == "operators" || s == "skills" || s == "equipped_weapons" || s == "equipped_gear"
}
func nextNode(ctx *maa.Context, current, next string) bool {
	if err := ctx.OverrideNext(current, []maa.NextItem{{Name: next}}); err != nil {
		log.Error().Err(err).Msg("scan routing failed")
		return false
	}
	return true
}
func captureImage(ctx *maa.Context) (image.Image, error) {
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil, fmt.Errorf("tasker unavailable")
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return nil, fmt.Errorf("controller unavailable")
	}
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return nil, fmt.Errorf("scan image unavailable")
	}
	return img, nil
}

// signature ignores fine animation/noise while retaining portrait and text layout changes.
func signature(img image.Image, rect image.Rectangle) []float64 {
	out := []float64{}
	for y := rect.Min.Y; y < rect.Max.Y; y += 12 {
		for x := rect.Min.X; x < rect.Max.X; x += 8 {
			sum := 0.0
			n := 0.0
			for dy := 0; dy < 8 && y+dy < rect.Max.Y; dy += 2 {
				for dx := 0; dx < 6 && x+dx < rect.Max.X; dx += 2 {
					r, g, b, _ := img.At(x+dx, y+dy).RGBA()
					sum += float64(r+g+b) / 3 / 257
					n++
				}
			}
			out = append(out, sum/n)
		}
	}
	return out
}
func unchanged(a, b []float64) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	d := 0.0
	for i := range a {
		d += math.Abs(a[i] - b[i])
	}
	return d/float64(len(a)) < 5
}
func scanCommand(args ...string) ([]byte, error) {
	python, err := pythonPath()
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	argv := append([]string{fsutil.OutputPath("tools", "account_scan.py"), "--store", fsutil.OutputPath("data", "AccountScans")}, args...)
	cmd := exec.CommandContext(runCtx, python, argv...)
	configureCommand(cmd)
	cmd.Dir = filepath.Dir(fsutil.OutputPath("interface.json"))
	return cmd.CombinedOutput()
}

// Run holds only scalar/image-derived state across callbacks, never framework handles.
func (a *BatchAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var p batchParam
	if json.Unmarshal([]byte(arg.CustomActionParam), &p) != nil {
		return false
	}
	batchMu.Lock()
	defer batchMu.Unlock()
	if p.Step == "begin" {
		if !coreTask(p.Task) {
			return false
		}
		if pienv.ControllerKind() != pienv.ControllerKindAdb {
			maafocus.Print(ctx, "Automatic roster traversal uses the verified emulator layout. Reading the currently open native page only.")
			return nextNode(ctx, arg.CurrentTaskName, legacyCaptureNode(p.Task))
		}
		if len(batchRuns) > 16 {
			batchRuns = map[int64]*batchRun{}
		}
		batchRuns[arg.TaskID] = &batchRun{Mode: p.Task, Resume: p.Resume}
		maafocus.Print(ctx, "Automatic roster capture started. No equipment or resources will be changed.")
		return true
	}
	run := batchRuns[arg.TaskID]
	if run == nil {
		return false
	}
	switch p.Step {
	case "prepare":
		if run.Prepared {
			return nextNode(ctx, arg.CurrentTaskName, "AccountBatchSnapshotOverview")
		}
		return nextNode(ctx, arg.CurrentTaskName, "AccountBatchOpenRosterGrid")
	case "grid_top":
		img, err := captureImage(ctx)
		if err != nil {
			return false
		}
		sig := signature(img, image.Rect(42, 80, 365, 600))
		if unchanged(run.GridBefore, sig) {
			run.Prepared = true
			return nextNode(ctx, arg.CurrentTaskName, "AccountBatchSelectFirst")
		}
		run.GridBefore = sig
		run.GridProbes++
		if run.GridProbes > 24 {
			maafocus.Print(ctx, "Roster top could not be verified; capture remains partial.")
			return false
		}
		return nextNode(ctx, arg.CurrentTaskName, "AccountBatchRosterScrollTop")
	case "matrix_align":
		img, err := captureImage(ctx)
		if err != nil {
			return false
		}
		points := p.Points
		if len(points) != 9 && len(points) != 13 {
			return false
		}
		reference, referenceEnd := points[1], points[4]
		label, err := ctx.RunRecognition("AccountBatchBasicRank", img, nil)
		if err != nil || label == nil || !label.Hit {
			return false
		}
		bx, by := label.Box.X()-17, label.Box.Y()+label.Box.Height()+13
		if bx < 280 || bx > 530 || by < 210 || by > 310 {
			return false
		}
		gx, gy := bx+reference[0]-points[0][0], by+reference[1]-points[0][1]
		origin, ok := matrixWhiteNode(img, image.Rect(gx-40, gy-38, gx+40, gy+38))
		if !ok {
			return false
		}
		gx, gy = origin.X+referenceEnd[0]-reference[0], origin.Y+referenceEnd[1]-reference[1]
		end, okEnd := matrixWhiteNode(img, image.Rect(gx-45, gy-40, gx+45, gy+40))
		if !okEnd {
			return false
		}
		dx := float64(referenceEnd[0] - reference[0])
		sx := float64(end.X-origin.X) / dx
		shear := float64(end.Y-origin.Y-referenceEnd[1]+reference[1]) / dx
		if sx < .85 || sx > 1.1 || shear < -.2 || shear > .15 {
			return false
		}
		override := map[string]any{}
		for i, point := range points {
			x := origin.X + int(math.Round(float64(point[0]-reference[0])*sx))
			y := origin.Y + point[1] - reference[1] + int(math.Round(float64(point[0]-reference[0])*shear))
			if i == 0 {
				x, y = bx, by
			}
			if i > 0 {
				refined, found := matrixNode(img, image.Rect(x-40, y-32, x+40, y+32).Intersect(img.Bounds()), i > 8)
				if !found {
					maafocus.Print(ctx, fmt.Sprintf("Visible matrix node %d could not be located for %s.", i, run.Name))
					return false
				}
				x, y = refined.X, refined.Y
			}
			name := "AccountBatchClickBasic"
			if i > 0 {
				name = fmt.Sprintf("AccountBatchTalentOpen%02d", i)
			}
			override[name] = map[string]any{"action": map[string]any{"type": "Click", "param": map[string]any{"target": maa.Rect{x - 2, y - 2, 4, 4}}}}
			// Retry stages use the same image-aligned node, never the generator fallback.
			for attempt := 1; attempt <= 2; attempt++ {
				override[fmt.Sprintf("%sRetry%d", name, attempt)] = override[name]
			}
		}
		if len(points) == 13 {
			override["AccountBatchTalentBack08"] = map[string]any{"next": []string{"AccountBatchTalentOpen09", "AccountBatchTalentBackRetry08"}}
			override["AccountBatchTalentBackRetry08"] = map[string]any{"next": []string{"AccountBatchTalentOpen09"}}
		}
		last := fmt.Sprintf("AccountBatchTalentBack%02d", len(points)-1)
		override[last] = map[string]any{"next": []string{"AccountBatchMatrixBack", "AccountBatchTalentBackRetry" + fmt.Sprintf("%02d", len(points)-1)}}
		override["AccountBatchTalentBackRetry"+fmt.Sprintf("%02d", len(points)-1)] = map[string]any{"next": []string{"AccountBatchMatrixBack"}}
		if err := ctx.OverridePipeline(override); err != nil {
			return false
		}
		log.Info().Str("operator", run.Name).Any("origin", origin).Any("end", end).Float64("x_scale", sx).Float64("shear", shear).Msg("matrix nodes aligned")
		return nextNode(ctx, arg.CurrentTaskName, "AccountBatchClickBasic")
	case "dispatch":
		if run.Resume && run.Session != "" {
			out, err := scanCommand("capture-ready", "--session", run.Session, "--task", run.Mode, "--entity", run.Name)
			var saved struct {
				Ready bool `json:"ready"`
			}
			if err != nil || json.Unmarshal(out, &saved) != nil {
				return false
			}
			if saved.Ready {
				maafocus.Print(ctx, "Resuming: complete pages already saved for "+run.Name+" in this account session.")
				return nextNode(ctx, arg.CurrentTaskName, "AccountBatchOperatorDone")
			}
		}
		destinations := map[string]string{"operators": "AccountBatchOperatorDone", "skills": "AccountBatchOpenAbilities", "equipped_weapons": "AccountBatchOpenWeapons", "equipped_gear": "AccountBatchOpenGear"}
		return nextNode(ctx, arg.CurrentTaskName, destinations[run.Mode])
	case "operator_done":
		if run.Name == "" || len(run.Seen) >= 100 {
			return false
		}
		for _, name := range run.Seen {
			if name == run.Name {
				maafocus.Print(ctx, "Unexpected repeated operator; scan stopped without claiming full coverage.")
				return false
			}
		}
		run.Seen = append(run.Seen, run.Name)
		maafocus.Print(ctx, fmt.Sprintf("Captured %s (%d operators).", run.Name, len(run.Seen)))
		return nextNode(ctx, arg.CurrentTaskName, "AccountBatchAdvance")
	case "advance", "advance_after_scroll":
		img, err := captureImage(ctx)
		if err != nil {
			return false
		}
		box, ok := selectedPortrait(img)
		if !ok {
			maafocus.Print(ctx, "Selected operator highlight was not found; scan remains partial.")
			return false
		}
		target := maa.Rect{box.X() + 15, box.Y() + 109, box.Width() - 42, box.Height() - 49}
		if target.Y()+target.Height()/2 < 550 {
			target = maa.Rect{target.X() + target.Width()/2 - 2, target.Y() + target.Height()/2 - 2, 4, 4}
			run.RailProbes = 0
			changed := `^(?!` + regexp.QuoteMeta(run.Name) + `\s*/).+/\s*Overview$`
			override := map[string]any{
				"AccountBatchClickNext":   map[string]any{"action": map[string]any{"type": "Click", "param": map[string]any{"target": target}}},
				"AccountBatchChangedName": map[string]any{"recognition": map[string]any{"type": "OCR", "param": map[string]any{"roi": []int{0, 0, 550, 80}, "expected": []string{changed}}}},
			}
			if err := ctx.OverridePipeline(override); err != nil {
				return false
			}
			return nextNode(ctx, arg.CurrentTaskName, "AccountBatchClickNext")
		}
		sig := signature(img, image.Rect(0, 80, 70, 590))
		if p.Step == "advance_after_scroll" && unchanged(run.RailBefore, sig) {
			return nextNode(ctx, arg.CurrentTaskName, "AccountBatchFinish")
		}
		run.RailBefore = sig
		run.RailProbes++
		if run.RailProbes > 3 {
			maafocus.Print(ctx, "Roster end could not be verified; capture remains partial.")
			return false
		}
		return nextNode(ctx, arg.CurrentTaskName, "AccountBatchRailScroll")
	case "scroll_start":
		img, err := captureImage(ctx)
		if err != nil {
			return false
		}
		run.ScrollBefore = signature(img, image.Rect(922, 340, 1200, 602))
		run.ScrollProbes = 0
		run.ScrollPage = p.Page
		run.ScrollNext = p.Next
		return nextNode(ctx, arg.CurrentTaskName, "AccountBatchRightScroll")
	case "scroll_capture":
		target := "AccountBatchGearScrolled"
		if run.Mode == "equipped_weapons" {
			target = "AccountBatchWeaponScrolled"
		}
		return nextNode(ctx, arg.CurrentTaskName, target)
	case "scroll_check":
		img, err := captureImage(ctx)
		if err != nil {
			return false
		}
		sig := signature(img, image.Rect(922, 340, 1200, 602))
		if unchanged(run.ScrollBefore, sig) {
			return nextNode(ctx, arg.CurrentTaskName, run.ScrollNext)
		}
		run.ScrollBefore = sig
		run.ScrollProbes++
		if run.ScrollProbes >= 12 {
			maafocus.Print(ctx, "Detail-panel end was not verified; capture remains partial.")
			return false
		}
		return nextNode(ctx, arg.CurrentTaskName, "AccountBatchRightScroll")
	case "finish":
		if len(run.Seen) == 0 || run.Session == "" {
			return false
		}
		names, _ := json.Marshal(run.Seen)
		out, err := scanCommand("capture-complete", "--session", run.Session, "--task", run.Mode, "--entities", string(names))
		if err != nil {
			log.Error().Err(err).Str("detail", string(out)).Msg("capture coverage validation failed")
			return false
		}
		maafocus.Print(ctx, fmt.Sprintf("Capture complete: %d owned operators. OCR data is saved for review; this does not approve inventory or spending.", len(run.Seen)))
		delete(batchRuns, arg.TaskID)
		return true
	}
	return false
}
func legacyCaptureNode(task string) string {
	return map[string]string{"operators": "AccountScanOperatorsCapture", "skills": "AccountScanSkillsCapture", "equipped_weapons": "AccountScanEquippedWeaponsCapture", "equipped_gear": "AccountScanEquippedGearCapture"}[task]
}

func batchCaptureContext(id int64, page string, tokens []screenocr.Token) (string, string, string, error) {
	batchMu.Lock()
	defer batchMu.Unlock()
	run := batchRuns[id]
	if run == nil {
		return "", "", page, nil
	}
	if page == "scroll" {
		page = fmt.Sprintf("%s-scroll-%d", run.ScrollPage, run.ScrollProbes)
	}
	if page == "overview" {
		name := ""
		for _, token := range tokens {
			if token.Score >= .8 && token.Box[1] < 80 && strings.Contains(token.Text, "/") && strings.HasSuffix(strings.TrimSpace(token.Text), "Overview") {
				name = strings.TrimSpace(strings.SplitN(token.Text, "/", 2)[0])
				break
			}
		}
		if name == "" {
			// Entrance effects can cover the breadcrumb while the large name remains clear.
			for _, token := range tokens {
				if token.Score >= .9 && token.Box[0] >= 160 && token.Box[0] < 550 && token.Box[1] >= 100 && token.Box[1] < 175 && token.Box[3] >= 25 && regexp.MustCompile(`^[A-Za-z][A-Za-z '\-]+$`).MatchString(token.Text) {
					name = strings.TrimSpace(token.Text)
					break
				}
			}
		}
		if name == "" {
			return "", "", "", fmt.Errorf("operator header not readable")
		}
		if name == run.Name {
			return "", "", "", fmt.Errorf("operator did not change; scan remains partial")
		}
		run.Name = name
	}
	uid := ""
	for _, token := range tokens {
		if token.Score >= .8 {
			if parts := uidPattern.FindStringSubmatch(token.Text); len(parts) > 1 {
				uid = parts[1]
				break
			}
		}
	}
	if uid != "" {
		if run.UID != "" && run.UID != uid {
			return "", "", "", fmt.Errorf("account UID changed during capture")
		}
		run.UID = uid
	}
	if run.Name == "" || run.UID == "" {
		return "", "", "", fmt.Errorf("operator name and account UID must be read before capturing details")
	}
	return run.Name, run.UID, page, nil
}
func recordBatchSession(id int64, sid string) {
	batchMu.Lock()
	defer batchMu.Unlock()
	if run := batchRuns[id]; run != nil {
		run.Session = sid
	}
}
func hasBatch(id int64) bool { batchMu.Lock(); defer batchMu.Unlock(); return batchRuns[id] != nil }

// selectedPortrait finds the yellow selection bar beside the mobile portrait rail.
// A long contiguous bar is required so portrait artwork cannot count as selection.
func selectedPortrait(img image.Image) (maa.Rect, bool) {
	start, length := 0, 0
	for y := 80; y < 590; y++ {
		hit := 0
		for x := 71; x < 76; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r/257 > 220 && g/257 > 185 && b/257 < 110 {
				hit++
			}
		}
		if hit >= 3 {
			if length == 0 {
				start = y
			}
			length++
		} else {
			if (length >= 45 || (start > 520 && start+length >= 575 && length >= 15)) && length <= 90 {
				return maa.Rect{0, start - 5, 80, 85}, true
			}
			length = 0
		}
	}
	if (length >= 45 || (start > 520 && start+length >= 575 && length >= 15)) && length <= 90 {
		return maa.Rect{0, start - 5, 80, 85}, true
	}
	return maa.Rect{}, false
}

// matrixWhiteNode finds the bright glyph mass inside a bounded observed node area.
func matrixWhiteNode(img image.Image, rect image.Rectangle) (image.Point, bool) {
	return matrixNode(img, rect, false)
}

func matrixNode(img image.Image, rect image.Rectangle, dark bool) (image.Point, bool) {
	best, nBest := image.Point{}, 0
	white := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		if dark {
			return r/257 < 130 && g/257 < 130 && b/257 < 130
		}
		return r/257 > 200 && g/257 > 200 && b/257 > 170
	}
	for y := rect.Min.Y + 10; y < rect.Max.Y-10; y += 2 {
		for x := rect.Min.X + 10; x < rect.Max.X-10; x += 2 {
			n := 0
			for dy := -12; dy <= 12; dy++ {
				for dx := -12; dx <= 12; dx++ {
					if white(x+dx, y+dy) {
						n++
					}
				}
			}
			if n > nBest {
				best = image.Pt(x, y)
				nBest = n
			}
		}
	}
	if nBest < 10 {
		return image.Point{}, false
	}
	sumX, sumY, n := 0, 0, 0
	for y := best.Y - 14; y <= best.Y+14; y++ {
		for x := best.X - 14; x <= best.X+14; x++ {
			if white(x, y) {
				sumX += x
				sumY += y
				n++
			}
		}
	}
	return image.Pt(sumX/n, sumY/n), true
}
