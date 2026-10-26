package innkeeping

import (
	"image"
	"math"
)

// routeAlignment records how one measured joystick pulse displaced both
// architectural patches. It is tied to this route's camera and departure pose.
type routeAlignment struct {
	Command movementCommand `json:"command"`
	Offsets [2][2]int       `json:"offsets"`
}

func validRouteAlignment(a *routeAlignment) bool {
	if a == nil || a.Command.Kind != "walk" || a.Command.Milliseconds != 20 || a.Command.HoldMilliseconds < 20 || a.Command.HoldMilliseconds > 250 || a.Command.X < 100 || a.Command.X > 290 || a.Command.Y < 462 || a.Command.Y > 640 || (a.Command.X-195)*(a.Command.X-195)+(a.Command.Y-551)*(a.Command.Y-551) < 1600 {
		return false
	}
	for _, p := range a.Offsets {
		if p[0]*p[0]+p[1]*p[1] < 36 || abs(p[0]) > 24 || abs(p[1]) > 12 {
			return false
		}
	}
	return true
}

// locateRouteScene reports nearby landmarks for steering, never permission to
// execute a route. The original sceneMatchesImage still authorizes departure.
func locateRouteScene(img image.Image, reference sceneFingerprint) ([2][2]int, bool) {
	return locateRouteSceneWindow(img, reference, 24, 12)
}

func locateRouteSceneWindow(img image.Image, reference sceneFingerprint, horizontal, vertical int) ([2][2]int, bool) {
	var offsets [2][2]int
	for i, p := range reference {
		best := math.MaxFloat64
		for dy := -vertical; dy <= vertical; dy += 2 {
			for dx := -horizontal; dx <= horizontal; dx += 2 {
				box := p.Box
				box[0] += dx
				box[1] += dy
				current := sampleScene(img, box)
				current.Box = p.Box
				current.ArchitectureOnly = p.ArchitectureOnly
				if !scenePatchSetMatches([]scenePatch{current}, []scenePatch{p}) {
					continue
				}
				var lighting [3]float64
				n := 0
				for j, v := range current.Pixels {
					if p.ArchitectureOnly && !architecturePixel(p.Pixels, (j/3)*3) {
						continue
					}
					lighting[j%3] += float64(int(v) - int(p.Pixels[j]))
					n++
				}
				for channel := range lighting {
					lighting[channel] /= float64(n / 3)
				}
				sum := 0.0
				for j, v := range current.Pixels {
					if p.ArchitectureOnly && !architecturePixel(p.Pixels, (j/3)*3) {
						continue
					}
					sum += math.Abs(float64(int(v)-int(p.Pixels[j])) - lighting[j%3])
				}
				if score := sum / float64(n); score < best {
					best = score
					offsets[i] = [2]int{dx, dy}
				}
			}
		}
		if best == math.MaxFloat64 {
			return offsets, false
		}
	}
	return offsets, true
}

func planRouteAlignment(img image.Image, r fixedRoute) (movementCommand, float64, bool) {
	if !validRouteAlignment(r.Alignment) {
		return movementCommand{}, 0, false
	}
	offsets, ok := locateRouteScene(img, r.Start)
	if !ok {
		return movementCommand{}, 0, false
	}
	return alignmentCorrection(offsets, *r.Alignment)
}

func routeDepartureNearby(img image.Image, r fixedRoute) bool {
	if sceneMatchesImage(img, r.Start) {
		return true
	}
	if r.CounterHub && validSeatMarker(r.StartSeatMarker) {
		// Candidate only: the action callback must read this physical table
		// number again before moving toward the reviewed counter staging point.
		if r.SeatTemplate != "" {
			_, ok := locatePhysicalSeatTemplate(img, r.SeatTemplate, r.StartSeatMarker)
			return ok
		}
		_, ok := locateRouteScene(img, r.Start)
		return ok
	}
	_, _, ok := planRouteAlignment(img, r)
	return ok
}

func alignmentCorrection(offsets [2][2]int, a routeAlignment) (movementCommand, float64, bool) {
	var dot, responseNorm, errorNorm float64
	var scales [2]float64
	for i, p := range offsets {
		dx, dy := float64(a.Offsets[i][0]), float64(a.Offsets[i][1])
		ex, ey := float64(p[0]), float64(p[1])
		denom := dx*dx + dy*dy
		if denom < 36 {
			return movementCommand{}, 0, false
		}
		scales[i] = -(ex*dx + ey*dy) / denom
		// A displacement perpendicular to the measured response needs a
		// different calibration, not an assumed screen-axis correction.
		rx, ry := ex+scales[i]*dx, ey+scales[i]*dy
		if rx*rx+ry*ry > 36 {
			return movementCommand{}, 0, false
		}
		dot += ex*dx + ey*dy
		responseNorm += denom
		errorNorm += ex*ex + ey*ey
	}
	if scales[0]*scales[1] <= 0 || errorNorm < 16 {
		return movementCommand{}, 0, false
	}
	scale := -dot / responseNorm
	cmd := a.Command
	cmd.Pipeline = true
	// Mobile movement has an actuation floor: scaling a measured hold down
	// can merely turn the character. Replay the actual measured pulse, with
	// fresh visual feedback and a strict two-pulse bound instead of extrapolation.
	// A measured direction does not calibrate its reverse. Also reject a
	// full pulse when its measured response predicts a worse residual; the
	// actuation floor makes scaling such a pulse down unreliable.
	if scale <= 0 {
		return movementCommand{}, 0, false
	}
	var residual float64
	for i, p := range offsets {
		x, y := float64(p[0]+a.Offsets[i][0]), float64(p[1]+a.Offsets[i][1])
		residual += x*x + y*y
	}
	if residual >= errorNorm {
		return movementCommand{}, 0, false
	}
	return cmd, math.Sqrt(errorNorm), true
}
