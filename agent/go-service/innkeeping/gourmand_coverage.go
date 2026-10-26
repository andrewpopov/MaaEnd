package innkeeping

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"image"
)

// A parsed prefix is insufficient: visible text ink outside recognized line
// boxes may contain a missing exclusion. Such a frame cannot authorize Serve.
func gourmandTextCovered(img image.Image, bubble image.Rectangle, tokens []screenocr.Token) bool {
	if img == nil || bubble.Empty() {
		return false
	}
	area := image.Rect(bubble.Min.X+45, bubble.Min.Y+4, bubble.Max.X-8, bubble.Max.Y-6).Intersect(img.Bounds())
	ink, uncovered, rows := 0, 0, 0
	for y := area.Min.Y; y < area.Max.Y; y++ {
		// Trim the rounded bubble corners using the gold backdrop on this row.
		left, right := area.Max.X, area.Min.X
		for x := area.Min.X; x < area.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 >= 210 && g>>8 >= 170 && b>>8 >= 70 && b>>8 <= 190 && r > g {
				left = min(left, x)
				right = max(right, x)
			}
		}
		missing := 0
		for x := left + 2; x < right-2; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r >>= 8
			g >>= 8
			b >>= 8
			if !(r < 175 && g < 165 && b < 145 || r >= 180 && g < 175 && b < 120 && r > g+50) {
				continue
			}
			ink++
			covered := false
			for _, t := range tokens {
				if t.Score >= .90 && x >= t.Box[0]-2 && x <= t.Box[0]+t.Box[2]+2 && y >= t.Box[1]-2 && y <= t.Box[1]+t.Box[3]+2 {
					covered = true
					break
				}
			}
			if !covered {
				uncovered++
				missing++
			}
		}
		if missing >= 4 {
			rows++
		}
	}
	return ink >= 40 && !(uncovered >= 24 && rows >= 3)
}
