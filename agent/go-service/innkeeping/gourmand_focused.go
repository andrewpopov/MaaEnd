package innkeeping

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"image"
	"regexp"
	"sort"
	"strings"
)

var gourmandCounterText = regexp.MustCompile(`\b[0-9]+/[0-9]*\s*`)

// Restrict popup text to its observed pale gold backdrop. This excludes world
// interaction labels and the grey quest panel when their coordinates overlap.
func gourmandBubbleTokens(img image.Image, tokens []screenocr.Token) []screenocr.Token {
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) {
		return nil
	}
	var result []screenocr.Token
	for _, t := range tokens {
		b := t.Box
		if b[0] < 50 || b[0]+b[2] >= 1280 || b[1] < 185 || b[1]+b[3] >= 470 || b[2] < 4 || b[3] < 8 {
			continue
		}
		gold := 0
		for _, p := range [][2]int{{b[0] + b[2]/2, b[1] + 2}, {b[0] + b[2]/2, b[1] + b[3] - 2}, {b[0] + 2, b[1] + b[3]/2}, {b[0] + b[2] - 2, b[1] + b[3]/2}} {
			r, g, blue, _ := img.At(p[0], p[1]).RGBA()
			if r>>8 >= 210 && g>>8 >= 170 && blue>>8 >= 70 && blue>>8 <= 190 && r > g {
				gold++
			}
		}
		if gold >= 2 {
			result = append(result, t)
		}
	}
	return result
}

// Locate the connected gold backdrop, including words that full-frame OCR
// merged into the quest labels. Table surfaces do not qualify without request text.
func gourmandBubbleBounds(img image.Image, tokens []screenocr.Token) image.Rectangle {
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) {
		return image.Rectangle{}
	}
	const width = 1280
	mask := make([]bool, width*470)
	for y := 185; y < 470; y++ {
		for x := 50; x < 1280; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			mask[y*width+x] = r>>8 >= 210 && g>>8 >= 170 && b>>8 >= 70 && b>>8 <= 190 && r > g
		}
	}
	found := image.Rectangle{}
	for y := 185; y < 470; y++ {
		for x := 50; x < 1280; x++ {
			seed := y*width + x
			if !mask[seed] {
				continue
			}
			mask[seed] = false
			queue := []int{seed}
			bounds := image.Rect(x, y, x+1, y+1)
			for at := 0; at < len(queue); at++ {
				q := queue[at]
				qx, qy := q%width, q/width
				bounds = bounds.Union(image.Rect(qx, qy, qx+1, qy+1))
				for _, n := range []int{q - 1, q + 1, q - width, q + width} {
					if n >= 185*width && n < 470*width && n%width >= 50 && abs(n%width-qx)+abs(n/width-qy) == 1 && mask[n] {
						mask[n] = false
						queue = append(queue, n)
					}
				}
			}
			if len(queue) < 2000 || bounds.Dx() < 140 || bounds.Dx() > 650 || bounds.Dy() < 35 || bounds.Dy() > 180 {
				continue
			}
			supported := false
			for _, t := range tokens {
				if len(strings.Fields(t.Text)) >= 2 && image.Pt(t.Box[0]+t.Box[2]/2, t.Box[1]+t.Box[3]/2).In(bounds) {
					supported = true
					break
				}
			}
			if supported {
				if !found.Empty() {
					return image.Rectangle{}
				}
				found = bounds
			}
		}
	}
	return found
}

// Focused line OCR recovers words split by the translucent quest overlay.
// It reads only this fresh image; unknown text still cannot authorize a recipe.
func focusedGourmandRequests(img image.Image, tokens []screenocr.Token, seats []SeatView, catalog []DishDefinition, read func(maa.Rect) ([]screenocr.Token, error)) []GourmandRequestView {
	requests, _ := readFocusedGourmand(img, tokens, seats, catalog, read)
	return requests
}

func readFocusedGourmand(img image.Image, tokens []screenocr.Token, seats []SeatView, catalog []DishDefinition, read func(maa.Rect) ([]screenocr.Token, error)) ([]GourmandRequestView, []screenocr.Token) {
	anyGold := false
	for _, seat := range seats {
		anyGold = anyGold || seat.Phase == "gourmand"
	}
	if !anyGold {
		return nil, nil
	}
	candidates := gourmandBubbleTokens(img, tokens)
	bubble := gourmandBubbleBounds(img, candidates)
	if !bubble.Empty() {
		candidates = nil
		for _, t := range tokens {
			if image.Pt(t.Box[0]+t.Box[2]/2, t.Box[1]+t.Box[3]/2).In(bubble) {
				candidates = append(candidates, t)
			}
		}
	}
	var lines [][]screenocr.Token
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Box[1]+candidates[i].Box[3]/2 < candidates[j].Box[1]+candidates[j].Box[3]/2
	})
	left, right := 1280, 0
	for _, t := range candidates {
		text := strings.TrimSpace(t.Text)
		if text == "" || gourmandCounterText.ReplaceAllString(text, "") == "" || len(text) == 1 && text >= "1" && text <= "6" {
			continue
		}
		cy := t.Box[1] + t.Box[3]/2
		if len(lines) == 0 || abs(cy-(lines[len(lines)-1][0].Box[1]+lines[len(lines)-1][0].Box[3]/2)) > 10 {
			lines = append(lines, nil)
		}
		lines[len(lines)-1] = append(lines[len(lines)-1], t)
		left = min(left, t.Box[0])
		right = max(right, t.Box[0]+t.Box[2])
	}
	if !bubble.Empty() {
		left = bubble.Min.X + 45
		right = bubble.Max.X - 8
	}
	if len(lines) == 0 || len(lines) > 6 || right-left > 600 {
		return nil, nil
	}
	var focused []screenocr.Token
	for i, line := range lines {
		top, bottom := 720, 0
		for _, t := range line {
			top = min(top, t.Box[1]-3)
			bottom = max(bottom, t.Box[1]+t.Box[3]+3)
		}
		center := line[0].Box[1] + line[0].Box[3]/2
		if i > 0 {
			p := lines[i-1][0]
			top = max(top, (center+p.Box[1]+p.Box[3]/2)/2)
		}
		if i+1 < len(lines) {
			p := lines[i+1][0]
			bottom = min(bottom, (center+p.Box[1]+p.Box[3]/2)/2)
		}
		if bottom-top < 14 {
			return nil, nil
		}
		region := maa.Rect{left - 1, top, right - left + 3, bottom - top}
		found, e := read(region)
		if e != nil || len(found) != 1 || found[0].Score < .90 {
			return nil, nil
		}
		t := found[0]
		t.Text = strings.TrimSpace(gourmandCounterText.ReplaceAllString(t.Text, ""))
		t.Box = [4]int(region)
		focused = append(focused, t)
	}
	requests := readGourmandRequestsCount(focused, gourmandPortions(img, seats))
	for i := range requests {
		requests[i].Candidates = MatchGourmandRecipes(requests[i].Requirements, catalog)
	}
	return requests, focused
}
