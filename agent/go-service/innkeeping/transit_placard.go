package innkeeping

import "image"

type placardGlyph struct {
	Box    image.Rectangle
	Pixels int
}

func creamPixel(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	r >>= 8
	g >>= 8
	b >>= 8
	return r >= 150 && g >= 120 && b >= 65 && r-g < 75 && g-b < 95
}
func brownPixel(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	r >>= 8
	g >>= 8
	b >>= 8
	return r >= 25 && r <= 180 && g >= 15 && g <= 145 && b <= 115 && r >= g+8 && g >= b+5
}

// placardNumeral identifies one bounded connected cream glyph. It deliberately
// does not assign a number; fresh OCR must independently establish that identity.
func placardNumeral(img image.Image) (placardGlyph, bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 16 || w > 60 || h < 24 || h > 80 {
		return placardGlyph{}, false
	}
	seen := make([]bool, w*h)
	var best placardGlyph
	second := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if seen[i] || !creamPixel(img, b.Min.X+x, b.Min.Y+y) {
				continue
			}
			seen[i] = true
			queue := []int{i}
			box := image.Rect(x, y, x+1, y+1)
			for head := 0; head < len(queue); head++ {
				v := queue[head]
				cx, cy := v%w, v/w
				box = box.Union(image.Rect(cx, cy, cx+1, cy+1))
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						nx, ny := cx+dx, cy+dy
						if nx < 0 || nx >= w || ny < 0 || ny >= h {
							continue
						}
						n := ny*w + nx
						if !seen[n] && creamPixel(img, b.Min.X+nx, b.Min.Y+ny) {
							seen[n] = true
							queue = append(queue, n)
						}
					}
				}
			}
			if len(queue) > best.Pixels {
				second = best.Pixels
				best = placardGlyph{box, len(queue)}
			} else {
				second = max(second, len(queue))
			}
		}
	}
	return best, best.Pixels >= 10 && second*2 < best.Pixels && best.Box.Dx() >= 2 && best.Box.Dy() >= 8 && best.Box.Min.X > 0 && best.Box.Min.Y > 0 && best.Box.Max.X < w && best.Box.Max.Y < h
}

func brownBand(img image.Image, box image.Rectangle) float64 {
	box = box.Intersect(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	if box.Empty() {
		return 0
	}
	n := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if brownPixel(img, img.Bounds().Min.X+x, img.Bounds().Min.Y+y) {
				n++
			}
		}
	}
	return float64(n) / float64(box.Dx()*box.Dy())
}

func glyphBands(g image.Rectangle) []image.Rectangle {
	return []image.Rectangle{
		image.Rect(g.Min.X-3, g.Min.Y, g.Min.X-1, g.Max.Y),
		image.Rect(g.Max.X+1, g.Min.Y, g.Max.X+3, g.Max.Y),
		image.Rect(g.Min.X, g.Min.Y-3, g.Max.X, g.Min.Y-1),
		image.Rect(g.Min.X, g.Max.Y+1, g.Max.X, g.Max.Y+3),
	}
}

// placardMatches retains the calibrated glyph scale and surrounding brown frame
// when lighting or perspective prevents photometric template matching. This is
// only a geometry check inside the architecture-predicted ROI, never a search.
func placardMatches(current, reference image.Image) bool {
	if current == nil || reference == nil || current.Bounds().Size() != reference.Bounds().Size() {
		return false
	}
	a, ok := placardNumeral(current)
	if !ok {
		return false
	}
	b, ok := placardNumeral(reference)
	if !ok {
		return false
	}
	for _, size := range [][2]int{{a.Box.Dx(), b.Box.Dx()}, {a.Box.Dy(), b.Box.Dy()}} {
		if size[0]*100 < size[1]*80 || size[0]*100 > size[1]*125 {
			return false
		}
	}
	if abs(a.Box.Min.X+a.Box.Max.X-b.Box.Min.X-b.Box.Max.X) > 6 || abs(a.Box.Min.Y+a.Box.Max.Y-b.Box.Min.Y-b.Box.Max.Y) > 6 {
		return false
	}
	for i, band := range glyphBands(a.Box) {
		baseline := brownBand(reference, glyphBands(b.Box)[i])
		if baseline < .3 || brownBand(current, band) < max(.3, baseline*.65) {
			return false
		}
	}
	return true
}
