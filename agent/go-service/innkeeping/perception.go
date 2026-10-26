package innkeeping

import (
	"encoding/json"
	"fmt"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/draw"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

// IconLibrary contains dish exemplars captured from the actual order cards.
// Unknown dishes remain unknown; a nearest match alone is never sufficient.
type IconLibrary map[string]image.Image

// Scalers contain geometry, not screenshots. x/image Scaler is concurrency-safe;
// a bounded cache reuses its kernel weights and temporary-buffer pool.
var iconScalerCache [65]struct {
	once   sync.Once
	scaler xdraw.Scaler
}

func cachedIconScaler(radius int) xdraw.Scaler {
	if radius < 8 || radius >= len(iconScalerCache) {
		return xdraw.CatmullRom.NewScaler(24, 24, 2*radius, 2*radius)
	}
	entry := &iconScalerCache[radius]
	entry.once.Do(func() { entry.scaler = xdraw.CatmullRom.NewScaler(24, 24, 2*radius, 2*radius) })
	return entry.scaler
}

// iconExemplar keeps immutable matching samples beside its original capture.
// Reference normalization is independent of the live frame and only happens once.
type iconExemplar struct {
	image.Image
	samples []uint8
}

func iconSamples(im *image.RGBA, out []uint8) []uint8 {
	out = out[:0]
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			if (float64(x)-11.5)*(float64(x)-11.5)+(float64(y)-11.5)*(float64(y)-11.5) > 95 {
				continue
			}
			c := im.RGBAAt(x, y)
			out = append(out, c.R, c.G, c.B)
		}
	}
	return out
}

// LoadIcons loads the calibrated exemplars beside the recipe catalog.
func LoadIcons(folder string) (IconLibrary, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	result := IconLibrary{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".png" {
			continue
		}
		f, err := os.Open(filepath.Join(folder, e.Name()))
		if err != nil {
			return nil, err
		}
		im, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		result[strings.TrimSuffix(e.Name(), ".png")] = &iconExemplar{Image: im, samples: iconSamples(normalizedIcon(im, im.Bounds()), nil)}
	}
	if len(result) < 2 {
		return nil, fmt.Errorf("at least two dish exemplars are required")
	}
	return result, nil
}

func normalizedIcon(src image.Image, r image.Rectangle) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, 24, 24))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, r, draw.Src, nil)
	return dst
}
func iconDistance(src image.Image, r image.Rectangle, ref image.Image) float64 {
	if !r.In(src.Bounds()) {
		return 255
	}
	a, b := normalizedIcon(src, r), normalizedIcon(ref, ref.Bounds())
	sum, n := 0.0, 0.0
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			if math.Pow(float64(x)-11.5, 2)+math.Pow(float64(y)-11.5, 2) > 95 {
				continue
			}
			ca, cb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			sum += math.Abs(float64(int(ca.R)-int(cb.R))) + math.Abs(float64(int(ca.G)-int(cb.G))) + math.Abs(float64(int(ca.B)-int(cb.B)))
			n += 3
		}
	}
	return sum / n
}

// Identify rejects ambiguous and out-of-distribution icons. Offsets and sizes
// account for the measured active, kitchen, queue and tray renderings.
func (lib IconLibrary) Identify(img image.Image, cx, cy, radius int) (string, float64) {
	if !colorful(img, cx, cy, min(radius, 20)) {
		return "", 0
	}
	type reference struct {
		id     string
		pixels []uint8
	}
	refs := []reference{}
	best := map[string]float64{}
	for key, ref := range lib {
		id := strings.Split(key, "--")[0]
		samples := []uint8(nil)
		if cached, ok := ref.(*iconExemplar); ok {
			samples = cached.samples
		} else {
			samples = iconSamples(normalizedIcon(ref, ref.Bounds()), nil)
		}
		refs = append(refs, reference{id, samples})
		best[id] = 255
	}
	dst := image.NewRGBA(image.Rect(0, 0, 24, 24))
	var sampleBuffer [900]uint8
	// Most HUD and tray icons are already centered. Accept only a strong,
	// unambiguous exact-position match before paying for the full offset search.
	center := image.Rect(cx-radius, cy-radius, cx+radius, cy+radius)
	if center.In(img.Bounds()) {
		cachedIconScaler(radius).Scale(dst, dst.Bounds(), img, center, draw.Src, nil)
		candidate := iconSamples(dst, sampleBuffer[:0])
		for _, ref := range refs {
			sum := 0
			for i, v := range candidate {
				sum += abs(int(v) - int(ref.pixels[i]))
			}
			best[ref.id] = math.Min(best[ref.id], float64(sum)/float64(len(candidate)))
		}
		if id, score := confidentIcon(best, 18, 10); id != "" {
			return id, score
		}
	}
	scalers := map[int]xdraw.Scaler{}
	for r := radius - 4; r <= radius+4; r += 2 {
		if r >= 8 {
			scalers[r] = cachedIconScaler(r)
		}
	}
	for dy := -4; dy <= 4; dy += 2 {
		for dx := -4; dx <= 4; dx += 2 {
			for r := radius - 4; r <= radius+4; r += 2 {
				rect := image.Rect(cx+dx-r, cy+dy-r, cx+dx+r, cy+dy+r)
				if r < 8 || !rect.In(img.Bounds()) {
					continue
				}
				scalers[r].Scale(dst, dst.Bounds(), img, rect, draw.Src, nil)
				candidate := iconSamples(dst, sampleBuffer[:0])
				for _, ref := range refs {
					sum := 0
					for i, v := range candidate {
						sum += abs(int(v) - int(ref.pixels[i]))
					}
					best[ref.id] = math.Min(best[ref.id], float64(sum)/float64(len(candidate)))
				}
			}
		}
	}
	return confidentIcon(best, 27, 6)
}

func confidentIcon(best map[string]float64, maximum, margin float64) (string, float64) {
	type match struct {
		id       string
		distance float64
	}
	matches := []match{}
	for id, d := range best {
		matches = append(matches, match{id, d})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].distance < matches[j].distance })
	if len(matches) < 2 || strings.HasPrefix(matches[0].id, "__") || matches[0].distance > maximum || matches[1].distance-matches[0].distance < margin {
		return "", 0
	}
	return matches[0].id, 1 - matches[0].distance/255
}

// SeatView preserves each independent food request and the diner phase.
type SeatView struct {
	Seat   string   `json:"seat"`
	Phase  string   `json:"phase"`
	Dishes []string `json:"dishes"`
	Served []string `json:"served,omitempty"`
}

// RecipeView binds a recognized dish, displayed cook time and confirmed stock to its card.
type RecipeView struct {
	Dish      string  `json:"dish"`
	Seconds   float64 `json:"seconds"`
	Available bool    `json:"available"`
	Box       [4]int  `json:"box"`
}

// LiveView contains measured controls only for the calibrated 720p Android layout.
type LiveView struct {
	TrayCapacity           int                   `json:"tray_capacity,omitempty"`
	Phase                  string                `json:"phase"`
	Kitchen                string                `json:"kitchen,omitempty"`
	Seats                  []SeatView            `json:"seats"`
	Tray                   map[string]int        `json:"tray"`
	TraySlots              []string              `json:"tray_slots"`
	SelectedSlot           int                   `json:"selected_slot"`
	Recipes                []RecipeView          `json:"recipes,omitempty"`
	Queue                  map[string]int        `json:"queue,omitempty"`
	ActiveDish             string                `json:"active_dish,omitempty"`
	ActiveSeconds          float64               `json:"active_seconds,omitempty"`
	QueueVerified          bool                  `json:"queue_verified"`
	RemainingSeconds       int                   `json:"remaining_seconds"`
	Prompt                 string                `json:"prompt,omitempty"`
	PromptBox              [4]int                `json:"prompt_box,omitempty"`
	PromptDish             string                `json:"prompt_dish,omitempty"`
	AddOrderBox            [4]int                `json:"add_order_box,omitempty"`
	Unknown                []string              `json:"unknown,omitempty"`
	GourmandSeat           string                `json:"gourmand_seat,omitempty"`
	GourmandBoards         []screenocr.Token     `json:"-"`
	GourmandBubble         [4]int                `json:"gourmand_bubble,omitempty"`
	GourmandTargetVerified bool                  `json:"gourmand_target_verified,omitempty"`
	GourmandTextComplete   bool                  `json:"gourmand_text_complete,omitempty"`
	GourmandRequests       []GourmandRequestView `json:"gourmand_requests,omitempty"`
}

func foldName(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, strings.ToLower(s))
}

// kitchenSpecialties reads the currently selected cooks' Operator UP panel.
// Themes retain different chef assignments; a kitchen number cannot imply a bonus.
func kitchenSpecialties(tokens []screenocr.Token) []string {
	var result []string
	for _, token := range tokens {
		if token.Score < .90 || token.Box[0] < 80 || token.Box[0] >= 400 || token.Box[1] < 400 || token.Box[1] >= 500 {
			continue
		}
		category := foldName(token.Text)
		if (category == "entree" || category == "side" || category == "dessert" || category == "soup") && !includes(result, category) {
			result = append(result, category)
		}
	}
	return result
}
func greenBadge(img image.Image, x, y int) bool {
	n := 0
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			r, g, b, _ := img.At(x+dx, y+dy).RGBA()
			if g>>8 > 150 && g > r && g > b*3/2 {
				n++
			}
		}
	}
	return n > 30
}
func colorful(img image.Image, x, y, radius int) bool {
	sum, n := 0.0, 0.0
	for dy := -radius; dy <= radius; dy += 3 {
		for dx := -radius; dx <= radius; dx += 3 {
			r, g, b, _ := img.At(x+dx, y+dy).RGBA()
			hi := max(r, g, b) >> 8
			lo := min(r, g, b) >> 8
			sum += float64(hi - lo)
			n++
		}
	}
	return sum/n > 8
}

var secondsPattern = regexp.MustCompile(`(?i)^(\d+)s$`)
var clockPattern = regexp.MustCompile(`(\d{1,2})[:：](\d{2})$`)

// ReadLive recognizes visible orders, held food and current-kitchen cards without
// guessing the contents of the other kitchen or the off-screen food counter.
// expandedGuestCard distinguishes an actual food card from the collapsed seat
// header. Without this boundary, world scenery below an empty header can match
// a dish exemplar (notably the brown soup icon).
func expandedGuestCard(img image.Image, left, width, y int) bool {
	for _, x := range []int{left + 4, left + width - 6} {
		r, g, b, _ := img.At(x, y+30).RGBA()
		lo, hi := min(r, g, b)>>8, max(r, g, b)>>8
		if lo < 180 || hi-lo > 40 {
			return false
		}
	}
	return true
}

func ReadLive(img image.Image, tokens []screenocr.Token, lib IconLibrary, dishes []DishDefinition) LiveView {
	return readLivePhase(img, tokens, lib, dishes, Observe(tokens).Phase)
}

// readLivePhase shares visual inventory recognition with locally validated world
// controls. Its caller must independently verify the phase on the fresh image.
func readLivePhase(img image.Image, tokens []screenocr.Token, lib IconLibrary, dishes []DishDefinition, phase string) LiveView {
	v := LiveView{Phase: phase, Tray: map[string]int{}, TraySlots: make([]string, 5), SelectedSlot: -1, Queue: map[string]int{}}
	if img == nil || img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 720 {
		return v
	}
	names := map[string]string{}
	for _, d := range dishes {
		names[foldName(d.Name)] = d.ID
	}
	kitchen := v.Phase == "kitchen"
	if kitchen {
		for i, x := range []int{150, 430} {
			r, g, b, _ := img.At(x, 157).RGBA()
			if r > g*2 && r > b*2 && r>>8 > 110 {
				v.Kitchen = strconv.Itoa(i + 1)
			}
		}
	}
	if v.Phase != "active" && v.Phase != "closing" && v.Phase != "closed_serving" && !kitchen {
		return v
	}
	for seat := 0; seat < 6; seat++ {
		x0, stride, y := 234, 118, 70
		if kitchen {
			x0, stride, y = 234, 171, 47
		}
		left := x0 + seat*stride
		cardWidth := 118
		if kitchen {
			cardWidth = 171
		}
		s := SeatView{Seat: strconv.Itoa(seat + 1), Phase: "unknown"}
		for _, t := range tokens {
			if t.Score < .85 || t.Box[0] < left || t.Box[0] >= left+cardWidth || t.Box[1] > 120 {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(t.Text)) {
			case "ordering":
				s.Phase = "ordering"
			case "dining":
				s.Phase = "dining"
			case "bill ready":
				s.Phase = "bill_ready"
			case "gourmand":
				s.Phase = "gourmand"
			}
		}
		// Gold Gourmand cards cannot be interpreted as ordinary food requests.
		r, g, b, _ := img.At(left+8, y+15).RGBA()
		if r>>8 > 210 && g>>8 > 170 && b>>8 < 170 && s.Phase == "unknown" {
			s.Phase = "gourmand"
		}
		if s.Phase == "unknown" && (kitchen || expandedGuestCard(img, left, cardWidth, y)) {
			centers := []int{left + 30, left + 85}
			if kitchen {
				centers = []int{left + 78, left + 132}
			}
			for _, x := range centers {
				if greenBadge(img, x+14, y+18) {
					if id, _ := lib.Identify(img, x, y, 18); id != "" {
						s.Served = append(s.Served, id)
					}
					continue
				}
				if id, _ := lib.Identify(img, x, y, 18); id != "" {
					s.Dishes = append(s.Dishes, id)
				}
			}
			if len(s.Dishes) == 0 {
				// A one-item order is centered; the two-item layout leaves that
				// position between its separate icons. Do not count it twice.
				x := (centers[0] + centers[1]) / 2
				if !greenBadge(img, x+14, y+18) {
					if id, _ := lib.Identify(img, x, y, 18); id != "" {
						s.Dishes = append(s.Dishes, id)
					}
				} else if id, _ := lib.Identify(img, x, y, 18); id != "" {
					s.Served = append(s.Served, id)
				}
			}
			if len(s.Dishes) > 0 {
				s.Phase = "waiting"
			} else if len(s.Served) > 0 {
				s.Phase = "served"
			}
		}
		v.Seats = append(v.Seats, s)
	}
	for _, t := range tokens {
		if t.Score < .85 {
			continue
		}
		text := strings.TrimSpace(t.Text)
		if m := clockPattern.FindStringSubmatch(text); m != nil && t.Box[0] < 220 && t.Box[1] < 120 {
			a, _ := strconv.Atoi(m[1])
			b, _ := strconv.Atoi(m[2])
			v.RemainingSeconds = a*60 + b
		}
		if !kitchen && text == "Add Order" {
			v.AddOrderBox = t.Box
		}
	}
	if !kitchen {
		v.GourmandRequests = readGourmandRequestsCount(gourmandBubbleTokens(img, tokens), gourmandPortions(img, v.Seats))
		for i := range v.GourmandRequests {
			v.GourmandRequests[i].Candidates = MatchGourmandRecipes(v.GourmandRequests[i].Requirements, dishes)
		}
		readWorldPrompt(&v, tokens, names)
		v.SelectedSlot = selectedTraySlot(img)
		for slot := 0; slot < 5; slot++ {
			x := 483 + 79*slot
			if id, _ := lib.Identify(img, x, 623, 28); id != "" {
				v.Tray[id]++
				v.TraySlots[slot] = id
			} else if colorful(img, x, 623, 20) {
				v.Unknown = append(v.Unknown, fmt.Sprintf("tray slot %d", slot+1))
			}
		}
		return v
	}
	for row := 0; row < 3; row++ {
		for col := 0; col < 2; col++ {
			left, top := 451+col*383, 171+row*116
			r := RecipeView{Box: [4]int{left + 145, top + 20, 150, 55}}
			for _, t := range tokens {
				if t.Box[0] < left || t.Box[0] >= left+375 || t.Box[1] < top || t.Box[1] >= top+110 {
					continue
				}
				// The hourglass occasionally becomes a Yen glyph (observed ￥34S).
				// Accept that variant only inside the measured time badge.
				if t.Score >= .75 && t.Box[0] < left+85 && t.Box[1] < top+30 {
					if m := secondsPattern.FindStringSubmatch(strings.TrimLeft(t.Text, "￥¥ ")); m != nil {
						r.Seconds, _ = strconv.ParseFloat(m[1], 64)
					}
				}
				if t.Score < .85 {
					continue
				}
				if id := names[foldName(t.Text)]; id != "" {
					r.Dish = id
				} else if t.Box[0] >= left+120 && t.Box[0]+t.Box[2] > left+340 && t.Box[1] < top+45 {
					// The title and its adjacent price can be one OCR token
					// (recorded: "Shroom Dumpling32"). Match the entire dish
					// name plus only a numeric price in that exact title field.
					for name, id := range names {
						price, ok := strings.CutPrefix(foldName(t.Text), name)
						if !ok || len(price) == 0 || len(price) > 3 {
							continue
						}
						if value, err := strconv.Atoi(price); err == nil && value > 0 {
							r.Dish = id
							break
						}
					}
				}
				if m := secondsPattern.FindStringSubmatch(t.Text); m != nil {
					r.Seconds, _ = strconv.ParseFloat(m[1], 64)
				}
				if strings.HasPrefix(t.Text, "Preparable:") {
					s := strings.TrimSpace(strings.TrimPrefix(t.Text, "Preparable:"))
					n, e := strconv.Atoi(s)
					r.Available = s == "∞" || (e == nil && n > 0)
				}
			}
			if r.Dish != "" {
				for _, d := range dishes {
					if d.ID == r.Dish && d.BaseCookSeconds > 0 && r.Seconds > d.BaseCookSeconds*2 {
						r.Seconds = 0
					}
				}
				v.Recipes = append(v.Recipes, r)
			}
		}
	}
	for _, token := range tokens {
		if token.Score >= .8 && token.Box[0] >= 310 && token.Box[0] < 390 && token.Box[1] >= 600 && token.Box[1] < 640 {
			if m := secondsPattern.FindStringSubmatch(token.Text); m != nil {
				v.ActiveSeconds, _ = strconv.ParseFloat(m[1], 64)
			}
		}
	}
	v.QueueVerified = true
	// Active work rises above the rightmost slot; waiting slots retain their centers.
	for _, p := range [][3]int{{343, 575, 30}, {241, 607, 27}, {145, 607, 27}} {
		if id, _ := lib.Identify(img, p[0], p[1], p[2]); id != "" {
			v.Queue[id]++
			if p[0] == 343 {
				v.ActiveDish = id
			}

		} else if colorful(img, p[0], p[1], 20) {
			v.QueueVerified = false
			v.Unknown = append(v.Unknown, "unrecognized kitchen queue item")
		}
	}
	return v
}

// readWorldPrompt adds fresh interaction text without repeating icon recognition.
func readWorldPrompt(v *LiveView, tokens []screenocr.Token, names map[string]string) {
	for _, t := range tokens {
		if t.Score < .85 || t.Box[0] < 720 || t.Box[1] < 390 || t.Box[1] >= 470 {
			continue
		}
		text := strings.TrimSpace(t.Text)
		switch {
		case text == "Serve":
			v.Prompt, v.PromptBox = "serve", t.Box
		case text == "Welcome":
			v.Prompt, v.PromptBox = "welcome", t.Box
		case text == "Bar":
			v.Prompt, v.PromptBox = "bar", t.Box
		case text == "Put Back on Counter":
			v.Prompt = "put_back"
		case names[foldName(text)] != "":
			v.Prompt, v.PromptDish, v.PromptBox = "pickup", names[foldName(text)], t.Box
		}
	}
	readWrappedPrompt(v, tokens, names)
}

// LoadCatalog reads recipe mechanics from the data bundle, never from inferred icons.
func LoadCatalog(path string) ([]DishDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Recipes []DishDefinition `json:"recipes"`
	}
	err = json.Unmarshal(data, &catalog)
	return catalog.Recipes, err
}

// greenServeControl tests the actual OCR text box, rather than one letter whose
// antialiasing changes while the interaction prompt animates.
func greenServeControl(img image.Image, box [4]int) bool {
	if box[2] <= 0 || box[3] <= 0 {
		return false
	}
	n := 0
	for y := box[1]; y < box[1]+box[3]; y++ {
		for x := box[0]; x < box[0]+box[2]; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if g>>8 > 150 && g > r && g > b*3/2 {
				n++
			}
		}
	}
	return n > max(12, box[2]*box[3]/25)
}

// selectedTraySlot reads the selected slot's white outline. The yellow arrow
// also marks the next pickup destination and is not a reliable selection signal.
func selectedTraySlot(img image.Image) int {
	best, bestN := -1, 100
	for slot := 0; slot < 5; slot++ {
		left := 445 + 79*slot
		n := 0
		for y := 589; y < 663; y++ {
			for x := left; x < left+75; x++ {
				if !(y >= 591 && y < 660 && (x < left+3 || x >= left+72)) && !(x >= left+5 && x < left+70 && (y < 592 || y >= 660)) {
					continue
				}
				r, g, b, _ := img.At(x, y).RGBA()
				r >>= 8
				g >>= 8
				b >>= 8
				if min(r, g, b) > 190 && max(r, g, b)-min(r, g, b) < 30 {
					n++
				}
			}
		}
		if n > bestN {
			best, bestN = slot, n
		}
	}
	return best
}

// readWrappedPrompt matches the entire food name across the actual two-line
// interaction label; long names such as Salted Cheese Crackers wrap in this UI.
func readWrappedPrompt(v *LiveView, tokens []screenocr.Token, names map[string]string) {
	parts := []screenocr.Token{}
	for _, t := range tokens {
		if t.Score >= .85 && t.Box[0] >= 800 && t.Box[0] < 1120 && t.Box[1] >= 390 && t.Box[1] < 470 {
			duplicate := false
			for i, previous := range parts {
				if foldName(previous.Text) == foldName(t.Text) && abs(previous.Box[0]-t.Box[0]) <= 8 && abs(previous.Box[1]-t.Box[1]) <= 8 {
					if t.Score > previous.Score {
						parts[i] = t
					}
					duplicate = true
					break
				}
			}
			if !duplicate {
				parts = append(parts, t)
			}
		}
	}
	if len(parts) < 2 {
		return
	}
	sort.Slice(parts, func(i, j int) bool {
		if abs(parts[i].Box[1]-parts[j].Box[1]) > 8 {
			return parts[i].Box[1] < parts[j].Box[1]
		}
		return parts[i].Box[0] < parts[j].Box[0]
	})
	words := []string{}
	left, top, right, bottom := 1280, 720, 0, 0
	for _, t := range parts {
		words = append(words, t.Text)
		left = min(left, t.Box[0])
		top = min(top, t.Box[1])
		right = max(right, t.Box[0]+t.Box[2])
		bottom = max(bottom, t.Box[1]+t.Box[3])
	}
	name := foldName(strings.Join(words, " "))
	if id := names[name]; id != "" {
		v.Prompt = "pickup"
		v.PromptDish = id
		v.PromptBox = [4]int{left, top, right - left, bottom - top}
	} else if name == "putbackoncounter" {
		v.Prompt = "put_back"
		v.PromptBox = [4]int{left, top, right - left, bottom - top}
	}
}

// Ingredient stock alone does not prove another request for the same recipe
// can be issued while it is already in this kitchen's visible queue.
func recipeQueueable(v LiveView, r RecipeView) bool {
	return r.Available && v.QueueVerified && v.Queue[r.Dish] == 0
}
