package innkeeping

import (
	"image"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

// GourmandRequestView preserves each serving's exact request independently.
// A request with unfamiliar wording remains unparsed and cannot authorize food.
type GourmandRequestView struct {
	Text         string              `json:"text"`
	Requirements Requirements        `json:"requirements"`
	Candidates   []GourmandCandidate `json:"candidates,omitempty"`
}

// readGourmandRequests reads the two recorded request lines after Welcome.
// The nearby theme counter can overlap the second line in OCR; only its exact
// observed 0/12 prefix is removed. It is not part of the customer's sentence.
func readGourmandRequests(tokens []screenocr.Token) []GourmandRequestView {
	// Compatibility reader for the recorded two-serving OCR fixtures. Live
	// frames instead use their gold bubble mask before the broader reader.
	var region []screenocr.Token
	for _, t := range tokens {
		if t.Box[0] >= 400 && t.Box[0] < 750 && t.Box[1] >= 185 && t.Box[1] < 320 {
			region = append(region, t)
		}
	}
	return readGourmandRequestsCount(region, 2)
}

func readGourmandRequestsCount(tokens []screenocr.Token, count int) []GourmandRequestView {
	var lines []screenocr.Token
	for _, t := range tokens {
		if t.Box[0] >= 50 && t.Box[0] < 1150 && t.Box[1] >= 185 && t.Box[1] < 450 {
			// The recorded physical table board overlaps the request ROI. Its
			// tall single digit is not a sentence or an ingredient condition.
			text := strings.TrimSpace(t.Text)
			if text == "0/1" || text == "0/12" {
				continue
			}
			if len(text) == 1 && text >= "1" && text <= "6" && t.Box[3] >= 35 {
				continue
			}
			if t.Score < .90 {
				return nil
			}
			lines = append(lines, t)
		}
	}
	if len(lines) < 1 || len(lines) > 6 {
		return nil
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Box[1] < lines[j].Box[1] })
	parse := func(group []screenocr.Token) (GourmandRequestView, bool) {
		var parts []string
		for i, t := range group {
			if i > 0 && t.Box[1]-group[i-1].Box[1] > 35 {
				return GourmandRequestView{}, false
			}
			part := strings.TrimSpace(t.Text)
			if i > 0 {
				part = strings.TrimSpace(strings.TrimPrefix(part, "0/12"))
			}
			parts = append(parts, part)
		}
		text := strings.Join(parts, " ")
		r := GourmandRequestView{Text: text, Requirements: ParseGourmandRequest(text)}
		return r, r.Requirements.Parsed
	}
	if count == 1 {
		r, ok := parse(lines)
		if ok {
			return []GourmandRequestView{r}
		}
		return nil
	}
	var result []GourmandRequestView
	// OCR wrapping varies with the camera. Accept only one complete partition
	// into the two physical serving requests; never compact a missing request.
	for cut := 1; cut < len(lines); cut++ {
		first, ok1 := parse(lines[:cut])
		second, ok2 := parse(lines[cut:])
		if ok1 && ok2 {
			if result != nil {
				return nil
			}
			result = []GourmandRequestView{first, second}
		}
	}
	return result
}

// GourmandCandidate is a catalog match, not proof that the menu has unlocked it.
type GourmandCandidate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

func MatchGourmandRecipes(r Requirements, catalog []DishDefinition) []GourmandCandidate {
	var matches []GourmandCandidate
	for _, d := range catalog {
		if r.Accepts(d) {
			matches = append(matches, GourmandCandidate{d.ID, d.Name, d.Category})
		}
	}
	return matches
}

// The recorded gold card shows one centered plate or two side-by-side plates.
// Read the plate backdrops above their glyphs, not the request text wrapping.
func gourmandPortions(img image.Image, seats []SeatView) int {
	expected := 0
	if img == nil {
		return 2
	}
	disk := func(x int) bool {
		r, g, b, _ := img.At(x, 54).RGBA()
		return r>>8 >= 200 && r>>8 <= 240 && g>>8 >= 175 && g>>8 <= 210 && b>>8 >= 85 && b>>8 <= 135
	}
	for _, seat := range seats {
		if seat.Phase != "gourmand" {
			continue
		}
		index := seatIndex(seat.Seat)
		if index < 0 {
			return 2
		}
		left := 234 + 118*index
		count := 0
		if disk(left+30) && disk(left+85) {
			count = 2
		} else if disk(left+59) && !disk(left+30) && !disk(left+85) {
			count = 1
		}
		if count == 0 || expected != 0 && expected != count {
			return 2
		}
		expected = count
	}
	if expected == 0 {
		return 2
	}
	return expected
}

// Serving counts belong to the board beneath this popup, never to another
// gold card elsewhere in the HUD. A sole gold card is enough for perception;
// the white-Serve input boundary still requires an independently verified board.
func gourmandRequestSeats(seats []SeatView, tokens []screenocr.Token, bubble [4]int) []SeatView {
	gold := map[string]SeatView{}
	for _, seat := range seats {
		if seat.Phase == "gourmand" {
			gold[seat.Seat] = seat
		}
	}
	found := ""
	for _, t := range tokens {
		id := strings.TrimSpace(t.Text)
		if _, ok := gold[id]; !ok || t.Score < .90 || !validSeatMarker(t.Box) || !gourmandBoardNearBubble(bubble, t.Box) {
			continue
		}
		if found != "" && found != id {
			return nil
		}
		found = id
	}
	if found != "" {
		return []SeatView{gold[found]}
	}
	if len(gold) == 1 {
		for _, seat := range gold {
			return []SeatView{seat}
		}
	}
	return nil
}
