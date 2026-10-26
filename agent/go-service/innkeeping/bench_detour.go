package innkeeping

import (
	"image"
	"time"
)

// aisleFloorFraction measures the low-saturation tan floor beside the player.
// The observed table-one blockage has 100% floor on the left and 53% on the
// right, where the bench crosses the path. Wood and the dark player are excluded.
func aisleFloorFraction(img image.Image, side int) float64 {
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) {
		return 0
	}
	left := 360
	if side > 0 {
		left = 740
	}
	floor, total := 0, 0
	for y := 480; y < 580; y += 4 {
		for x := left; x < left+180; x += 4 {
			r, g, b, _ := img.At(x, y).RGBA()
			r >>= 8
			g >>= 8
			b >>= 8
			if min(r, g, b) >= 70 && min(r, g, b) <= 220 && max(r, g, b)-min(r, g, b) < 65 && r >= g && g >= b {
				floor++
			}
			total++
		}
	}
	return float64(floor) / float64(total)
}

// benchDetour gives a proven stationary diner approach one visible aisle
// waypoint. It keeps the original leg deadline and never repeats the bypass.
func benchDetour(n *navigationState, img image.Image, now time.Time) (movementCommand, bool) {
	if n == nil || n.Goal != "diner" || n.Stationary < 3 || n.BenchAvoided || n.Last[2] <= 0 {
		return movementCommand{}, false
	}
	left, right := aisleFloorFraction(img, -1), aisleFloorFraction(img, 1)
	if max(left, right) < .75 {
		return movementCommand{}, false
	}
	side := -1
	if right >= .75 && (left < .75 || n.Last[0]+n.Last[2]/2 > 640) {
		side = 1
	}
	box := n.Last
	n.BenchAvoided = true
	n.Stationary = 0
	n.Last = [4]int{}
	n.Since = now
	return movementCommand{Kind: "round_table", X: 195 + side*80, Y: 551, Milliseconds: 650, Goal: "diner", Box: box}, true
}
