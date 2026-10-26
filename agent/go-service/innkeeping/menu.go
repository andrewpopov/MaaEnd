package innkeeping

import (
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type menuChoice struct {
	Box    [4]int
	Ready  bool
	Reason string
}

var menuCountPattern = regexp.MustCompile(`([0-6])/6$`)
var innMenuFoods = []string{"Hot & Sour Soup", "Tuber Fries", "Fin Dumpling", "Salted Cheese Crackers"}

var knownMenuFoods = map[string]string{
	"hot_sour_soup": "Hot & Sour Soup", "tuber_fries": "Tuber Fries",
	"fin_dumpling": "Fin Dumpling", "salted_cheese_crackers": "Salted Cheese Crackers",
	"meaty_pilaf": "Meaty Pilaf", "tonic_congee_greens": "Tonic Congee & Greens",
	"shroom_dumpling": "Shroom Dumpling",
}

// OCR glyph bounds may extend just outside a card. The text center must
// still belong to that card's measured stock field, never its neighbor's.
func menuStockField(t screenocr.Token, left, top int) bool {
	x, y := t.Box[0]+t.Box[2]/2, t.Box[1]+t.Box[3]/2
	return t.Score >= .85 && t.Box[2] > 0 && t.Box[3] > 0 && x >= left && x < left+380 && y >= top+70 && y < top+120
}

// savedMenuChoice preserves a recognized saved menu that the game marks valid.
// Stock must be readable for every selected dish before starting a real run.
func savedMenuChoice(img image.Image, tokens []screenocr.Token, lib IconLibrary, foodCount, drinkCount int) (menuChoice, bool) {
	if foodCount < 4 || foodCount > 6 || drinkCount < 3 || drinkCount > 6 {
		return menuChoice{}, false
	}
	for _, cy := range []int{210, 240, 321} {
		yellow := 0
		for y := cy - 8; y <= cy+8; y++ {
			for x := 86; x <= 102; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r>>8 > 190 && g>>8 > 165 && b>>8 < 140 {
					yellow++
				}
			}
		}
		if yellow < 100 {
			return menuChoice{}, false
		}
	}
	selected := map[string]bool{}
	for slot := 0; slot < foodCount; slot++ {
		id, _ := lib.Identify(img, 264+72*slot, 608, 24)
		if knownMenuFoods[id] == "" || selected[id] {
			return menuChoice{Reason: "Saved menu contains an unrecognized dish; no selection changed."}, true
		}
		selected[id] = true
	}
	seen := map[string]bool{}
	for _, t := range tokens {
		if t.Score < .90 || t.Box[1] < 145 || t.Box[1] > 505 {
			continue
		}
		id := ""
		for candidate, name := range knownMenuFoods {
			if selected[candidate] && foldName(name) == foldName(t.Text) {
				id = candidate
				break
			}
		}
		if id == "" {
			continue
		}
		col, row := (t.Box[0]-460)/385, (t.Box[1]-145)/118
		if col < 0 || col > 1 || row < 0 || row > 2 {
			continue
		}
		left, top := 460+col*385, 147+row*118
		available := false
		for _, stock := range tokens {
			if !menuStockField(stock, left, top) {
				continue
			}
			if strings.HasPrefix(strings.ToLower(stock.Text), "preparable:") {
				value := strings.TrimSpace(strings.SplitN(stock.Text, ":", 2)[1])
				count, _ := strconv.Atoi(value)
				available = count > 0 || strings.Contains(value, "\u221e")
			}
		}
		if !available {
			return menuChoice{Reason: fmt.Sprintf("Saved recipe has no verified preparable stock: %s", knownMenuFoods[id])}, true
		}
		seen[id] = true
	}
	if len(seen) != foodCount {
		return menuChoice{Box: [4]int{480, 90, 100, 40}}, true
	}
	return menuChoice{Ready: true}, true
}

func redBorder(img image.Image, x, y int) bool {
	hits := 0
	for d := 0; d < 25; d++ {
		r, g, b, _ := img.At(x, y+d).RGBA()
		if r>>8 > 110 && r > g*2 && r > b*2 {
			hits++
		}
	}
	return hits > 10
}

func drinksMenuVisible(tokens []screenocr.Token) bool {
	stocks := 0
	for _, token := range tokens {
		if token.Score >= .85 && token.Box[0] >= 450 && token.Box[1] >= 200 && token.Box[1] < 510 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(token.Text)), "stock:") {
			stocks++
		}
	}
	return stocks >= 3
}

func selectedDrinkStock(img image.Image, tokens []screenocr.Token, expected int) menuChoice {
	selected := 0
	for row := 0; row < 3; row++ {
		for col := 0; col < 2; col++ {
			left, top := 460+385*col, 147+118*row
			if !redBorder(img, left, top+28) {
				continue
			}
			selected++
			available := false
			for _, token := range tokens {
				if !menuStockField(token, left, top) || !strings.HasPrefix(strings.ToLower(token.Text), "stock:") {
					continue
				}
				value := strings.TrimSpace(strings.SplitN(token.Text, ":", 2)[1])
				count, _ := strconv.Atoi(value)
				available = count > 0 || value == "∞"
			}
			if !available {
				return menuChoice{Reason: "A selected drink has no verified available stock; no menu selection changed."}
			}
		}
	}
	if expected < 3 || selected != expected {
		return menuChoice{Reason: "Not all selected drinks and their stock are visible; inspect the saved drink selection."}
	}
	return menuChoice{Ready: true}
}

// planMenu selects the four calibrated foods and three stocked drinks. It uses
// actual card borders and the displayed selection counters, never click counts.
func planMenu(img image.Image, tokens []screenocr.Token, libraries ...IconLibrary) menuChoice {
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) {
		return menuChoice{Reason: "Expected the 1280x720 inn menu."}
	}
	menu := false
	foodCount, drinkCount := -1, -1
	drinksTab := false
	for _, t := range tokens {
		if t.Score < .85 {
			continue
		}
		name := foldName(t.Text)
		if name == "menudesign" || (name == "menureq" && t.Box[0] < 250 && t.Box[1] > 100 && t.Box[1] < 150) {
			menu = true
		}
		if t.Box[1] > 600 {
			if m := menuCountPattern.FindStringSubmatch(strings.ReplaceAll(t.Text, " ", "")); m != nil {
				n, _ := strconv.Atoi(m[1])
				if t.Box[0] < 250 {
					foodCount = n
				} else if t.Box[0] > 250 && t.Box[0] < 950 {
					drinkCount = n
				}
			}
		}
	}
	if !menu {
		return menuChoice{Reason: "Menu Design is not visible."}
	}
	if len(libraries) > 0 && !drinksMenuVisible(tokens) {
		if choice, handled := savedMenuChoice(img, tokens, libraries[0], foodCount, drinkCount); handled {
			return choice
		}
	}
	// Recipe names identify the food page; both header buttons can be dark.
	drinksTab = false
	selected := map[string]bool{}
	boxes := map[string][4]int{}
	for _, t := range tokens {
		if t.Score < .90 || t.Box[1] < 145 || t.Box[1] > 505 {
			continue
		}
		for _, name := range innMenuFoods {
			if foldName(t.Text) == foldName(name) {
				col := (t.Box[0] - 460) / 385
				row := (t.Box[1] - 145) / 118
				if col < 0 || col > 1 || row < 0 || row > 2 {
					continue
				}
				x, y := 460+col*385, 147+row*118
				selected[name] = redBorder(img, x, y+28)
				boxes[name] = [4]int{x + 135, y + 25, 180, 50}
				stockKnown, stockAvailable := false, false
				for _, stockToken := range tokens {
					if !menuStockField(stockToken, x, y) {
						continue
					}
					if strings.HasPrefix(strings.ToLower(stockToken.Text), "preparable:") {
						value := strings.TrimSpace(strings.SplitN(stockToken.Text, ":", 2)[1])
						count, err := strconv.Atoi(value)
						stockKnown = err == nil || strings.Contains(value, "\u221e")
						stockAvailable = count > 0 || strings.Contains(value, "\u221e")
					}
				}
				if !stockKnown || !stockAvailable {
					return menuChoice{Reason: fmt.Sprintf("Required recipe has no verified preparable stock: %s", name)}
				}
			}
		}
	}
	drinksTab = len(boxes) == 0
	if foodCount < 0 || drinkCount < 0 {
		return menuChoice{Reason: "Waiting for readable dish and drink selection counts."}
	}
	if drinksTab {
		if drinkCount == 3 {
			return menuChoice{Box: [4]int{470, 90, 150, 40}}
		}
		if drinkCount > 3 {
			return menuChoice{Reason: "The saved menu has more than three drinks; inspect the drink tray before changing it."}
		}
		// Each available drink card has its own Preparable stock field.
		for row := 0; row < 3; row++ {
			for col := 0; col < 2; col++ {
				x, y := 460+col*385, 147+row*118
				if redBorder(img, x, y+28) {
					continue
				}
				available := false
				for _, t := range tokens {
					if t.Score > .85 && menuStockField(t, x, y) {
						if strings.HasPrefix(strings.ToLower(t.Text), "preparable:") || strings.HasPrefix(strings.ToLower(t.Text), "stock:") {
							stock := strings.TrimSpace(strings.SplitN(t.Text, ":", 2)[1])
							n, _ := strconv.Atoi(stock)
							available = n > 0 || strings.Contains(stock, "\u221e")
						}
					}
				}
				if available {
					return menuChoice{Box: [4]int{x + 135, y + 25, 180, 50}}
				}
			}
		}
		return menuChoice{Reason: "Three stocked drinks are not visible on the current menu page."}
	}
	desiredSelected := 0
	// The tray stays visible across category changes. Count its verified icons,
	// rather than treating selected recipes on another page as incompatible.
	if len(libraries) > 0 {
		traySelected := map[string]bool{}
		for slot := 0; slot < foodCount; slot++ {
			id, _ := libraries[0].Identify(img, 264+72*slot, 608, 24)
			name := map[string]string{"hot_sour_soup": "Hot & Sour Soup", "tuber_fries": "Tuber Fries", "fin_dumpling": "Fin Dumpling", "salted_cheese_crackers": "Salted Cheese Crackers"}[id]
			if name != "" {
				traySelected[name] = true
			}
		}
		if len(traySelected) == foodCount {
			selected = traySelected
		}
	}
	for _, ok := range selected {
		if ok {
			desiredSelected++
		}
	}
	if foodCount > desiredSelected {
		// Clear an incompatible saved menu one verified removal at a time. A zero
		// count is observed before rebuilding, so default recipes cannot leak in.
		return menuChoice{Box: [4]int{278 + (foodCount-1)*72, 574, 28, 28}}
	}
	for _, name := range innMenuFoods {
		if !selected[name] {
			if box, ok := boxes[name]; ok {
				return menuChoice{Box: box}
			}
			// Recipe pages are static categories; the next callback re-reads the
			// selected category and the tray before choosing a recipe.
			x := map[string]int{"Hot & Sour Soup": 1005, "Tuber Fries": 770, "Fin Dumpling": 650, "Salted Cheese Crackers": 890}[name]
			return menuChoice{Box: [4]int{x - 35, 90, 70, 35}}
		}
	}
	if foodCount != 4 {
		return menuChoice{Reason: "Four selected recipes have not been verified."}
	}
	if drinkCount != 3 {
		return menuChoice{Box: [4]int{1120, 90, 120, 40}}
	}
	return menuChoice{Ready: true}
}

var _ maa.CustomActionRunner = &InnMenuAction{}

// InnMenuAction handles one complex menu selection; Pipeline owns setup screens.
type InnMenuAction struct {
	TaskID       int64
	Last         [4]int
	At           time.Time
	Icons        IconLibrary
	FoodVerified bool
}

// verifiedMenuChoice finishes on the drink page, so no previously cached drink
// stock can authorize the start button. Food verification belongs to this task.
func (a *InnMenuAction) verifiedMenuChoice(img image.Image, tokens []screenocr.Token) menuChoice {
	if drinksMenuVisible(tokens) {
		if !a.FoodVerified {
			return planMenu(img, tokens, a.Icons)
		}
		count := -1
		for _, token := range tokens {
			if token.Score >= .85 && token.Box[1] > 600 && token.Box[0] > 250 && token.Box[0] < 950 {
				if m := menuCountPattern.FindStringSubmatch(strings.ReplaceAll(token.Text, " ", "")); m != nil {
					count, _ = strconv.Atoi(m[1])
				}
			}
		}
		return selectedDrinkStock(img, tokens, count)
	}
	d := planMenu(img, tokens, a.Icons)
	a.FoodVerified = d.Ready
	if d.Ready {
		return menuChoice{Box: [4]int{1120, 90, 120, 40}}
	}
	return d
}

// Run re-reads the menu, selects one observed card, or advances to the start node.
func (a *InnMenuAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
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
	im, e := ctrl.CacheImage()
	if e != nil || im == nil {
		return false
	}
	tokens, e := screenocr.Read(ctx, im)
	if e != nil {
		log.Error().Err(e).Int64("task", arg.TaskID).Msg("Native inn menu OCR failed")
		captureMenuDecision(arg.TaskID, im, nil, menuChoice{Reason: e.Error()})
		return false
	}
	if a.Icons == nil {
		a.Icons, e = LoadIcons(fsutil.OutputPath("data", "Innkeeping", "icons"))
		if e != nil {
			return false
		}
	}
	if a.TaskID != arg.TaskID {
		a.TaskID = arg.TaskID
		a.Last = [4]int{}
		a.FoodVerified = false
	}
	d := a.verifiedMenuChoice(im, tokens)
	log.Info().Int64("task", arg.TaskID).Interface("choice", d).Int("tokens", len(tokens)).Bool("food_verified", a.FoodVerified).Msg("Native inn menu decision")
	if d.Reason != "" {
		captureMenuDecision(arg.TaskID, im, tokens, d)
	}
	if d.Ready {
		a.Last = [4]int{}
		return route(ctx, arg, "InnkeepingMenuStart")
	}
	if d.Box[2] > 0 {
		if a.TaskID == arg.TaskID && a.Last == d.Box {
			if time.Since(a.At) > 5*time.Second {
				captureMenuDecision(arg.TaskID, im, tokens, d)
				maafocus.Print(ctx, "The menu selection did not change; stopped without clicking it again.")
				return false
			}
			return route(ctx, arg, "InnkeepingStartDispatch")
		}
		a.TaskID = arg.TaskID
		a.Last = d.Box
		a.At = time.Now()
		ctrl.PostClick(int32(d.Box[0]+d.Box[2]/2), int32(d.Box[1]+d.Box[3]/2)).Wait()
		return route(ctx, arg, "InnkeepingStartDispatch")
	}
	maafocus.Print(ctx, d.Reason)
	return false
}

// setupExpired makes an unknown setup scene stop with evidence, without opening
// another daily session or leaving the start dispatcher spinning indefinitely.
func (s *liveSession) setupExpired(now time.Time) bool {
	if s.SetupAt.IsZero() {
		s.SetupAt = now
	}
	return now.Sub(s.SetupAt) > 2*time.Minute
}

var _ maa.CustomActionRunner = &InnOpenAction{}

// InnOpenAction clicks the freshly recognized inn control after list stability.
// The interaction list shifts vertically when Si's dialogue becomes available.
type InnOpenAction struct{}

func innOpenControl(tokens []screenocr.Token) ([4]int, bool) {
	world := false
	box := [4]int{}
	for _, t := range tokens {
		if t.Score < .9 {
			continue
		}
		if foldName(t.Text) == "explore" && t.Box[1] < 70 {
			world = true
		}
		if foldName(t.Text) == "opentheinn" && t.Box[0] >= 700 && t.Box[1] >= 280 && t.Box[1] < 530 {
			box = t.Box
		}
	}
	return box, world && box[2] > 0
}

// Run re-reads the live interaction list instead of clicking a pre-freeze OCR box.
func (a *InnOpenAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
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
	im, e := ctrl.CacheImage()
	if e != nil || im == nil {
		return false
	}
	tokens, e := screenocr.Read(ctx, im)
	if e != nil {
		return false
	}
	box, ok := innOpenControl(tokens)
	if !ok {
		return route(ctx, arg, "InnkeepingStartDispatch")
	}
	ctrl.PostClick(int32(box[0]+box[2]/2), int32(box[1]+box[3]/2)).Wait()
	return route(ctx, arg, "InnkeepingAwaitTheme")
}

// Preserve the exact rejected frame and OCR rather than infer missing stock.
func captureMenuDecision(task int64, img image.Image, tokens []screenocr.Token, choice menuChoice) {
	session, err := newLiveSession()
	if err != nil {
		return
	}
	path, err := session.captureEvidence(time.Now(), img, "menu-validation-blocked")
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(map[string]any{"task_id": task, "image": path, "tokens": tokens, "decision": choice, "game_inputs_sent": false}, "", "  ")
	if err != nil {
		return
	}
	file := filepath.Join(session.CaptureRoot, fmt.Sprintf("menu-read-%d.json", task))
	if err = os.WriteFile(file, data, 0644); err != nil {
		log.Warn().Err(err).Msg("Save inn menu diagnostic")
	}
	log.Warn().Str("report", file).Interface("choice", choice).Msg("Inn menu validation stopped")
}
