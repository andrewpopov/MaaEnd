package autofight

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TeamMemberLevelAction validates one level transaction. Pipeline performs every
// navigation, increment and commit; this action never sends game input.
type TeamMemberLevelAction struct{}

type memberLevelState struct {
	Operator string            `json:"operator"`
	UID      string            `json:"account_uid"`
	Target   int               `json:"target_level"`
	Budget   int               `json:"currency_budget"`
	Spent    int               `json:"currency_spent"`
	Before   memberLevelScreen `json:"before"`
	LowCost  int               `json:"low_carrier_cost"`
	HighCost int               `json:"high_carrier_cost"`
	Cost     int               `json:"currency_cost"`
	Receipt  string            `json:"receipt"`
	Status   string            `json:"status"`
}

type memberLevelScreen struct {
	Level     int `json:"level"`
	Delta     int `json:"selected_levels"`
	Currency  int `json:"currency"`
	LowStock  int `json:"low_carrier_stock"`
	HighStock int `json:"high_carrier_stock"`
	LowCount  int `json:"low_carriers_selected"`
	HighCount int `json:"high_carriers_selected"`
	Cost      int `json:"currency_cost"`
}

var memberLevels = struct {
	sync.Mutex
	ByTask map[int64]*memberLevelState
}{ByTask: map[int64]*memberLevelState{}}
var levelNumber = regexp.MustCompile(`^[+0-9, ]+$`)
var levelUID = regexp.MustCompile(`(?i)UID\s*:?\s*(\d+)`)

func memberName(value string) string {
	return strings.NewReplacer(" ", "", "-", "", ":", "", "'", "").Replace(strings.ToLower(value))
}

func memberOverviewMatches(text, name string) bool {
	parts := strings.SplitN(text, "/", 2)
	return len(parts) == 2 && memberName(parts[0]) == memberName(name) && memberName(parts[1]) == "overview"
}

func readLevelNumber(ctx *maa.Context, img image.Image, roi maa.Rect) (int, error) {
	tokens, err := screenocr.ReadRegion(ctx, img, roi, true)
	if err != nil || len(tokens) != 1 || tokens[0].Score < 0.9 || !levelNumber.MatchString(tokens[0].Text) {
		return 0, fmt.Errorf("could not read exact level/quantity at %v", roi)
	}
	value := strings.NewReplacer("+", "", ",", "", " ", "").Replace(tokens[0].Text)
	return strconv.Atoi(value)
}

func readMemberLevelScreen(ctx *maa.Context, img image.Image) (memberLevelScreen, error) {
	var screen memberLevelScreen
	fields := []struct {
		Value *int
		ROI   maa.Rect
	}{
		{&screen.Level, maa.Rect{960, 117, 78, 53}},
		{&screen.Delta, maa.Rect{1040, 117, 78, 53}},
		{&screen.Currency, maa.Rect{1087, 20, 85, 31}},
		{&screen.LowStock, maa.Rect{232, 193, 72, 36}},
		{&screen.HighStock, maa.Rect{232, 293, 72, 36}},
		{&screen.LowCount, maa.Rect{190, 145, 65, 42}},
		{&screen.HighCount, maa.Rect{190, 245, 65, 42}},
		{&screen.Cost, maa.Rect{1171, 579, 75, 32}},
	}
	for _, field := range fields {
		value, err := readLevelNumber(ctx, img, field.ROI)
		if err != nil {
			return screen, err
		}
		*field.Value = value
	}
	return screen, nil
}

func memberLevelNext(ctx *maa.Context, current, next string) bool {
	return ctx.OverridePipeline(map[string]any{current: map[string]any{"next": []string{next}}}) == nil
}

func memberLevelOCR(roi []int, expected string) map[string]any {
	return map[string]any{"recognition": map[string]any{"type": "OCR", "param": map[string]any{"roi": roi, "expected": []string{expected}, "only_rec": true, "threshold": 0.9}}}
}

func memberLevelChecks(name string) []any {
	return []any{
		map[string]any{"recognition": map[string]any{"type": "OCR", "param": map[string]any{"roi": []int{75, 15, 550, 50}, "expected": []string{regexp.QuoteMeta(name) + `\s*/\s*Upgrade`}, "threshold": 0.9}}},
		map[string]any{"recognition": map[string]any{"type": "TemplateMatch", "param": map[string]any{"roi": []int{40, 145, 95, 90}, "template": "TeamManagement/CarrierLow.png", "threshold": 0.95}}},
		map[string]any{"recognition": map[string]any{"type": "TemplateMatch", "param": map[string]any{"roi": []int{40, 245, 95, 90}, "template": "TeamManagement/CarrierHigh.png", "threshold": 0.95}}},
	}
}

func validMemberLevelPreview(before, screen memberLevelScreen) bool {
	return screen.Level == before.Level && screen.Delta == 1 && screen.Currency == before.Currency && screen.LowStock == before.LowStock && screen.HighStock == before.HighStock && screen.Cost > 0 && screen.LowCount >= 0 && screen.HighCount >= 0 && screen.LowCount <= screen.LowStock && screen.HighCount <= screen.HighStock && screen.LowCount+screen.HighCount > 0
}

func validMemberLevelResult(state *memberLevelState, screen memberLevelScreen) bool {
	return screen.Level == state.Before.Level+1 && screen.Delta == 0 && screen.LowCount == 0 && screen.HighCount == 0 && screen.Cost == 0 && screen.Currency == state.Before.Currency-state.Cost && screen.LowStock == state.Before.LowStock-state.LowCost && screen.HighStock == state.Before.HighStock-state.HighCost
}

func (a *TeamMemberLevelAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	if _, err := fsutil.StartupDir(); err != nil {
		return false
	}
	var param struct {
		Step string `json:"step"`
	}
	if json.Unmarshal([]byte(arg.CustomActionParam), &param) != nil {
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
	memberLevels.Lock()
	defer memberLevels.Unlock()
	state := memberLevels.ByTask[arg.TaskID]
	if param.Step == "init" {
		settings, err := ctx.GetNode("TeamManagementSettings")
		if err != nil {
			return false
		}
		raw, err := json.Marshal(settings.Attach)
		if err != nil {
			return false
		}
		var options struct {
			TeamID   string `json:"team_id"`
			Operator string `json:"operator_name"`
			Target   int    `json:"operator_level"`
			Budget   int    `json:"currency_budget"`
		}
		if json.Unmarshal(raw, &options) != nil || options.Operator == "" || options.Target < 80 || options.Target > 90 || options.Budget < 0 {
			return false
		}
		var library struct {
			Teams []struct {
				ID      string   `json:"id"`
				Members []string `json:"members"`
			} `json:"teams"`
		}
		raw, err = os.ReadFile(fsutil.OutputPath("data", "TeamManagement", "library.json"))
		if err != nil || json.Unmarshal(raw, &library) != nil {
			return false
		}
		member := false
		for _, team := range library.Teams {
			if team.ID == options.TeamID {
				for _, name := range team.Members {
					if memberName(name) == memberName(options.Operator) {
						member = true
					}
				}
			}
		}
		if !member {
			maafocus.Print(ctx, "Choose a member of the selected team.")
			return false
		}
		tokens, err := screenocr.Read(ctx, img)
		if err != nil {
			return false
		}
		uid, identity := "", false
		for _, token := range tokens {
			if token.Score < 0.9 {
				continue
			}
			if match := levelUID.FindStringSubmatch(token.Text); len(match) > 1 {
				uid = match[1]
			}
			if token.Box[1] < 80 && memberOverviewMatches(token.Text, options.Operator) {
				identity = true
			}
		}
		var account struct {
			UID string `json:"uid_observed"`
		}
		raw, err = os.ReadFile(fsutil.OutputPath("data", "AccountScans", "ui_session.json"))
		if err != nil || json.Unmarshal(raw, &account) != nil || uid == "" || uid != account.UID || !identity {
			maafocus.Print(ctx, "Open the selected member's Overview and verify the scanned account UID.")
			return false
		}
		state = &memberLevelState{Operator: options.Operator, UID: uid, Target: options.Target, Budget: options.Budget, Status: "preview"}
		memberLevels.ByTask[arg.TaskID] = state
		currentLevel, err := readLevelNumber(ctx, img, maa.Rect{947, 105, 112, 78})
		if err != nil {
			return false
		}
		if currentLevel >= state.Target {
			delete(memberLevels.ByTask, arg.TaskID)
			return memberLevelNext(ctx, arg.CurrentTaskName, "TeamMemberLevelDone")
		}
		if currentLevel < 80 {
			maafocus.Print(ctx, "This verified level route supports Elite 4 levels 80–90 only; lower-level and promotion routes still need capture.")
			return false
		}
		patch := map[string]any{}
		for _, name := range []string{"TeamMemberLevelRead", "TeamMemberLevelIncrement", "TeamMemberLevelReview"} {
			patch[name] = map[string]any{"enabled": true, "recognition": map[string]any{"type": "And", "param": map[string]any{"all_of": memberLevelChecks(state.Operator)}}}
		}
		patch["TeamMemberLevelOpen"] = map[string]any{"enabled": true, "recognition": map[string]any{"type": "OCR", "param": map[string]any{"roi": []int{75, 15, 550, 50}, "expected": []string{regexp.QuoteMeta(state.Operator) + `\s*/\s*Overview`}, "threshold": 0.9}}}
		return ctx.OverridePipeline(patch) == nil
	}
	if state == nil {
		return false
	}
	screen, err := readMemberLevelScreen(ctx, img)
	if err != nil {
		maafocus.Print(ctx, err.Error())
		return false
	}
	switch param.Step {
	case "read":
		if screen.Level < 80 || screen.Level > 90 || screen.Delta != 0 || screen.LowCount != 0 || screen.HighCount != 0 || screen.Cost != 0 {
			return false
		}
		state.Before = screen
		if screen.Level >= state.Target || (state.Budget > 0 && state.Spent >= state.Budget) {
			delete(memberLevels.ByTask, arg.TaskID)
			return memberLevelNext(ctx, arg.CurrentTaskName, "TeamMemberLevelDone")
		}
		return memberLevelNext(ctx, arg.CurrentTaskName, "TeamMemberLevelIncrement")
	case "review":
		if !validMemberLevelPreview(state.Before, screen) {
			return false
		}
		previewDir := fsutil.OutputPath("data", "TeamManagement", "previews")
		if os.MkdirAll(previewDir, 0o700) != nil {
			return false
		}
		previewName := fmt.Sprintf("%s-%s-%d-%d", state.UID, memberName(state.Operator), screen.Level, screen.Level+1)
		previewImage := filepath.Join(previewDir, previewName+".png")
		imageFile, err := os.Create(previewImage)
		if err != nil {
			return false
		}
		imageErr := png.Encode(imageFile, img)
		imageCloseErr := imageFile.Close()
		if imageErr != nil || imageCloseErr != nil {
			return false
		}
		previewRecord := map[string]any{"id": previewName, "operator": state.Operator, "kind": "operator_level", "from": screen.Level, "to": screen.Level + 1, "account_uid": state.UID, "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "reviewed": false, "evidence": []string{filepath.ToSlash(filepath.Join("data", "TeamManagement", "previews", previewName+".png"))}, "costs": map[string]int{"T_CREDS": screen.Cost, "COGNITIVE_CARRIER_EXP": screen.LowCount*1000 + screen.HighCount*10000}, "stock": screen}
		previewRaw, err := json.MarshalIndent(previewRecord, "", "    ")
		if err != nil || fsutil.WriteFileAtomic(filepath.Join(previewDir, previewName+".json"), previewRaw, 0o600) != nil {
			return false
		}
		maafocus.Print(ctx, fmt.Sprintf("%s %d → %d: %d EXP (%d low + %d high carriers), %d T-Creds. Remaining task budget: %d.", state.Operator, screen.Level, screen.Level+1, screen.LowCount*1000+screen.HighCount*10000, screen.LowCount, screen.HighCount, screen.Cost, state.Budget-state.Spent))
		if screen.Cost > state.Budget-state.Spent || screen.Cost > screen.Currency {
			delete(memberLevels.ByTask, arg.TaskID)
			return memberLevelNext(ctx, arg.CurrentTaskName, "TeamMemberLevelDone")
		}
		state.LowCost, state.HighCost, state.Cost = screen.LowCount, screen.HighCount, screen.Cost
		dir := fsutil.OutputPath("data", "TeamManagement", "transactions")
		if os.MkdirAll(dir, 0o700) != nil {
			return false
		}
		state.Receipt = filepath.Join(dir, fmt.Sprintf("%s-%s-%d-%d.json", state.UID, memberName(state.Operator), screen.Level, screen.Level+1))
		state.Status = "armed_no_automatic_retry"
		raw, err := json.MarshalIndent(state, "", "    ")
		if err != nil {
			return false
		}
		file, err := os.OpenFile(state.Receipt, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			maafocus.Print(ctx, "A receipt already exists for this level. Review it before any retry.")
			return false
		}
		_, writeErr := file.Write(raw)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return false
		}
		checks := memberLevelChecks(state.Operator)
		checks = append(checks, memberLevelOCR([]int{960, 117, 78, 53}, fmt.Sprintf("^%d$", screen.Level)), memberLevelOCR([]int{1040, 117, 78, 53}, `^\+?1$`), memberLevelOCR([]int{1171, 579, 75, 32}, fmt.Sprintf("^%d$", screen.Cost)), memberLevelOCR([]int{190, 145, 65, 42}, fmt.Sprintf("^%d$", screen.LowCount)), memberLevelOCR([]int{190, 245, 65, 42}, fmt.Sprintf("^%d$", screen.HighCount)))
		checks = append(checks, memberLevelOCR([]int{1087, 20, 85, 31}, fmt.Sprintf("^%d$", screen.Currency)), memberLevelOCR([]int{232, 193, 72, 36}, fmt.Sprintf("^%d$", screen.LowStock)), memberLevelOCR([]int{232, 293, 72, 36}, fmt.Sprintf("^%d$", screen.HighStock)))
		checks = append(checks, memberLevelOCR([]int{85, 686, 240, 22}, `(?i)^UID\s*:\s*`+regexp.QuoteMeta(state.UID)+`$`))
		patch := map[string]any{"TeamMemberLevelCommit": map[string]any{"enabled": true, "recognition": map[string]any{"type": "And", "param": map[string]any{"all_of": checks}}}}
		verified := memberLevelChecks(state.Operator)
		verified = append(verified, memberLevelOCR([]int{960, 117, 78, 53}, fmt.Sprintf("^%d$", screen.Level+1)), memberLevelOCR([]int{1040, 117, 78, 53}, `^\+?0$`), memberLevelOCR([]int{1171, 579, 75, 32}, `^0$`), memberLevelOCR([]int{1087, 20, 85, 31}, fmt.Sprintf("^%d$", screen.Currency-screen.Cost)), memberLevelOCR([]int{232, 193, 72, 36}, fmt.Sprintf("^%d$", screen.LowStock-screen.LowCount)), memberLevelOCR([]int{232, 293, 72, 36}, fmt.Sprintf("^%d$", screen.HighStock-screen.HighCount)))
		patch["TeamMemberLevelVerify"] = map[string]any{"enabled": true, "recognition": map[string]any{"type": "And", "param": map[string]any{"all_of": verified}}}
		if ctx.OverridePipeline(patch) != nil {
			return false
		}
		return memberLevelNext(ctx, arg.CurrentTaskName, "TeamMemberLevelCommit")
	case "verify":
		if !validMemberLevelResult(state, screen) {
			maafocus.Print(ctx, "Upgrade result needs review; its armed receipt prevents automatic retries.")
			return false
		}
		state.Status = "verified"
		state.Spent += state.Cost
		raw, err := json.MarshalIndent(map[string]any{"transaction": state, "after": screen, "verified_at": time.Now().UTC().Format(time.RFC3339Nano)}, "", "    ")
		if err != nil || fsutil.WriteFileAtomic(state.Receipt, raw, 0o600) != nil {
			return false
		}
		file, err := os.Create(state.Receipt + ".png")
		if err != nil {
			return false
		}
		err = png.Encode(file, img)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return false
		}
		maafocus.Print(ctx, fmt.Sprintf("Verified %s level %d and exact material/currency deltas; spent %d / %d T-Creds.", state.Operator, screen.Level, state.Spent, state.Budget))
		if ctx.ClearHitCount("TeamMemberLevelCommit") != nil {
			return false
		}
		if ctx.OverridePipeline(map[string]any{"TeamMemberLevelCommit": map[string]any{"enabled": false}}) != nil {
			return false
		}
		return memberLevelNext(ctx, arg.CurrentTaskName, "TeamMemberLevelRead")
	}
	return false
}
