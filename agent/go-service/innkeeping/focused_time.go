package innkeeping

import (
	"image"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// readTimedLive uses the same measured timer fields at planning and input
// validation. Full-frame OCR can merge the hourglass with 11s into "311s",
// or lose the colon in 02:54. Never guess a duration from those tokens.
func readTimedLive(ctx *maa.Context, img image.Image, tokens []screenocr.Token, icons IconLibrary, catalog []DishDefinition) LiveView {
	v := ReadLive(img, tokens, icons, catalog)
	v = focusedLiveTimes(v, catalog, func(region maa.Rect) ([]screenocr.Token, error) {
		return screenocr.ReadRegion(ctx, img, region, true)
	})
	if v.Phase != "kitchen" {
		gold := gourmandBubbleTokens(img, tokens)
		bubble := gourmandBubbleBounds(img, gold)
		v.GourmandBubble = [4]int{bubble.Min.X, bubble.Min.Y, bubble.Dx(), bubble.Dy()}
		candidates := gourmandRequestSeats(v.Seats, tokens, v.GourmandBubble)
		if !bubble.Empty() {
			board := false
			for _, t := range tokens {
				for _, seat := range v.Seats {
					board = board || seat.Phase == "gourmand" && strings.TrimSpace(t.Text) == seat.Seat && t.Score >= .90 && validSeatMarker(t.Box) && gourmandBoardNearBubble(v.GourmandBubble, t.Box)
				}
			}
			if !board {
				region := image.Rect(bubble.Min.X+bubble.Dx()/2-160, bubble.Max.Y-20, bubble.Min.X+bubble.Dx()/2+160, bubble.Max.Y+110).Intersect(img.Bounds())
				v.GourmandBoards, _ = screenocr.ReadRegion(ctx, img, maa.Rect{region.Min.X, region.Min.Y, region.Dx(), region.Dy()}, false)
				candidates = gourmandRequestSeats(v.Seats, append(tokens, v.GourmandBoards...), v.GourmandBubble)
			}
		}
		v.GourmandRequests = nil
		if len(candidates) > 0 {
			v.GourmandRequests = readGourmandRequestsCount(gold, gourmandPortions(img, candidates))
			v.GourmandTextComplete = len(v.GourmandRequests) > 0 && gourmandTextCovered(img, bubble, gold)
			if !v.GourmandTextComplete {
				requests, lines := readFocusedGourmand(img, tokens, candidates, catalog, func(region maa.Rect) ([]screenocr.Token, error) { return screenocr.ReadRegion(ctx, img, region, true) })
				v.GourmandTextComplete = len(requests) > 0 && gourmandTextCovered(img, bubble, lines)
				if v.GourmandTextComplete {
					v.GourmandRequests = requests
				} else {
					v.GourmandRequests = nil
				}
			}
			for i := range v.GourmandRequests {
				v.GourmandRequests[i].Candidates = MatchGourmandRecipes(v.GourmandRequests[i].Requirements, catalog)
			}
		}
	}
	return v
}

func focusedLiveTimes(v LiveView, catalog []DishDefinition, read func(maa.Rect) ([]screenocr.Token, error)) LiveView {
	if v.RemainingSeconds == 0 && (v.Phase == "active" || v.Phase == "kitchen") {
		clockRegion := maa.Rect{95, 18, 68, 28}
		if v.Phase == "active" {
			clockRegion = maa.Rect{96, 92, 70, 30}
		}
		extra, err := read(clockRegion)
		if err == nil {
			for _, token := range extra {
				if token.Score < .90 {
					continue
				}
				if m := clockPattern.FindStringSubmatch(token.Text); m != nil {
					minutes, _ := strconv.Atoi(m[1])
					seconds, _ := strconv.Atoi(m[2])
					if minutes <= 5 && seconds < 60 {
						v.RemainingSeconds = minutes*60 + seconds
					}
				}
			}
		}
	}
	for i, recipe := range v.Recipes {
		if recipe.Seconds > 0 {
			continue
		}
		extra, err := read(maa.Rect{recipe.Box[0] - 118, recipe.Box[1] - 19, 34, 27})
		if err != nil {
			continue
		}
		for _, token := range extra {
			if token.Score < .90 {
				continue
			}
			if m := secondsPattern.FindStringSubmatch(token.Text); m != nil {
				seconds, _ := strconv.ParseFloat(m[1], 64)
				for _, dish := range catalog {
					if dish.ID == recipe.Dish && seconds > 0 && seconds <= dish.BaseCookSeconds*2 {
						v.Recipes[i].Seconds = seconds
					}
				}
			}
		}
	}
	return v
}
