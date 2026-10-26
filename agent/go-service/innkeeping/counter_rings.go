package innkeeping

import (
	"image"
	"math"
	"sort"
)

type ringProposal struct{ x, y, r, edges int }

// splitCounterRings inspects only a measured merged white component. A bright
// rim must transition to darker surroundings around most of its circumference;
// a diner capsule's rounded end lacks that outward boundary on its inner side.
func splitCounterRings(img image.Image, lib IconLibrary, box [4]int) []WorldFood {
	if box[2] < 40 || box[3] < 30 {
		return nil
	}
	// Ring fitting revisits each rim pixel many times. Sample image.Color once
	// per pixel rather than allocating/converting it inside every radial probe.
	region := image.Rect(box[0]-8, box[1]-8, box[0]+box[2]+8, box[1]+box[3]+8).Intersect(img.Bounds())
	mask := make([]bool, region.Dx()*region.Dy())
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r >>= 8
			g >>= 8
			b >>= 8
			mask[(y-region.Min.Y)*region.Dx()+x-region.Min.X] = min(r, g, b) >= 190 && max(r, g, b)-min(r, g, b) < 55
		}
	}
	bright := func(x, y int) bool {
		return image.Pt(x, y).In(region) && mask[(y-region.Min.Y)*region.Dx()+x-region.Min.X]
	}
	directions := [24][2]float64{}
	for i := range directions {
		a := float64(i) * math.Pi / 12
		directions[i] = [2]float64{math.Cos(a), math.Sin(a)}
	}
	proposals := []ringProposal{}
	for radius := 12; radius <= min(52, min(box[2], box[3])/2); radius += 2 {
		for cy := box[1] + radius; cy <= box[1]+box[3]-radius; cy += 2 {
			// The exposed front ring lies below the merged component's center.
			// Two icons inside one diner capsule share a horizontal baseline.
			// A connected pointer can extend the component down by several
			// pixels; allow that measured offset while retaining annulus checks.
			if cy*100 < box[1]*100+box[3]*50-800 {
				continue
			}
			for cx := box[0] + radius; cx <= box[0]+box[2]-radius; cx += 2 {
				rings, edges := 0, 0
				quadrants := [4]int{}
				for i, d := range directions {
					rim, edge := false, false
					for offset := -2; offset <= 2; offset++ {
						rr := float64(radius + offset)
						if !bright(cx+int(math.Round(d[0]*rr)), cy+int(math.Round(d[1]*rr))) {
							continue
						}
						rim = true
						outer := rr + 4
						if !bright(cx+int(math.Round(d[0]*outer)), cy+int(math.Round(d[1]*outer))) {
							edge = true
							break
						}
					}
					if rim {
						rings++
					}
					if edge {
						edges++
						quadrants[i/6]++
					}
				}
				if rings >= 21 && edges >= 16 && min(quadrants[0], quadrants[1], quadrants[2], quadrants[3]) >= 2 {
					proposals = append(proposals, ringProposal{cx, cy, radius, edges})
				}
			}
		}
	}
	sort.Slice(proposals, func(i, j int) bool {
		if proposals[i].edges != proposals[j].edges {
			return proposals[i].edges > proposals[j].edges
		}
		return proposals[i].r > proposals[j].r
	})
	result := []WorldFood{}
	classified := []ringProposal{}
	attempts := 0
	for _, p := range proposals {
		duplicate := false
		for _, q := range classified {
			if math.Hypot(float64(p.x-q.x), float64(p.y-q.y)) < math.Max(4, float64(min(p.r, q.r))*.35) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		attempts++
		if attempts > 8 {
			break
		}
		classified = append(classified, p)
		id, _ := lib.Identify(img, p.x, p.y-int(float64(p.r)*.2), int(float64(p.r)*.7))
		if id == "" {
			continue
		}
		result = append(result, WorldFood{Dish: id, Counter: true, Box: [4]int{p.x - p.r, p.y - p.r, 2 * p.r, 2 * p.r}})
		if len(result) >= 3 {
			break
		}
	}
	return result
}

// isolatedCounterBoundary rejects food-colored furniture fragments even when
// their interior accidentally resembles a calibrated dish icon. The annulus
// tolerates the observed small and Choice-marker offsets without accepting a fill.
func isolatedCounterBoundary(img image.Image, cx, cy, radius int) bool {
	return counterBoundary(img, cx, cy, radius, 190, 55)
}

func counterBoundary(img image.Image, cx, cy, radius, minimum, spread int) bool {
	bright := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		r >>= 8
		g >>= 8
		b >>= 8
		return min(r, g, b) >= uint32(minimum) && max(r, g, b)-min(r, g, b) < uint32(spread)
	}
	rings, edges := 0, 0
	quadrants := [4]int{}
	for i := 0; i < 24; i++ {
		a := float64(i) * math.Pi / 12
		dx, dy := math.Cos(a), math.Sin(a)
		rim, edge := false, false
		for offset := -3; offset <= 3; offset++ {
			r := float64(radius + offset)
			if !bright(cx+int(math.Round(dx*r)), cy+int(math.Round(dy*r))) {
				continue
			}
			rim = true
			if !bright(cx+int(math.Round(dx*(r+4))), cy+int(math.Round(dy*(r+4)))) {
				edge = true
			}
		}
		if rim {
			rings++
		}
		if edge {
			edges++
			quadrants[i/6]++
		}
	}
	return rings >= 16 && edges >= 14 && min(quadrants[0], quadrants[1], quadrants[2], quadrants[3]) >= 1
}

// A shaded arc can shift the connected white component's center a few pixels.
// Fit its boundary locally, retaining the original ring/edge/quadrant counts.
func shadedCounterBoundary(img image.Image, box [4]int) bool {
	cx, cy := box[0]+box[2]/2, box[1]+box[3]/2
	width := min(box[2], box[3])
	for dy := -4; dy <= 4; dy += 2 {
		for dx := -6; dx <= 6; dx += 2 {
			for radius := width * 46 / 100; radius <= width*56/100; radius += 2 {
				if counterBoundary(img, cx+dx, cy+dy, radius, 160, 25) {
					return true
				}
			}
		}
	}
	return false
}
