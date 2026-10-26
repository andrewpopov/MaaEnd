package innkeeping

import (
	"image"
	"math"
	"strings"
)

// routeInterval is an explicitly reviewed transit corridor in landmark space.
// Boundary offsets are measured against the route's reference image, separately
// for each landmark. Evidence identifies the two safe boundary photographs.
// It does not certify an interaction or general camera/physical pose equivalence.
type routeInterval struct {
	Boundaries              [2][2][2]int `json:"boundaries"` // boundary, landmark, x/y
	Evidence                [2]string    `json:"evidence"`
	CrossTrackPixels        float64      `json:"cross_track_pixels"`
	ProgressDifference      float64      `json:"progress_difference"`
	EndpointTolerancePixels float64      `json:"endpoint_tolerance_pixels,omitempty"`
}

func validRouteInterval(p *routeInterval) bool {
	if p == nil || strings.TrimSpace(p.Evidence[0]) == "" || strings.TrimSpace(p.Evidence[1]) == "" || strings.TrimSpace(p.Evidence[0]) == strings.TrimSpace(p.Evidence[1]) {
		return false
	}
	if math.IsNaN(p.CrossTrackPixels) || p.CrossTrackPixels <= 0 || p.CrossTrackPixels > 6 || math.IsNaN(p.ProgressDifference) || p.ProgressDifference <= 0 || p.ProgressDifference > .25 {
		return false
	}
	if math.IsNaN(p.EndpointTolerancePixels) || p.EndpointTolerancePixels < 0 || p.EndpointTolerancePixels > 6 {
		return false
	}
	for i := 0; i < 2; i++ {
		for b := 0; b < 2; b++ {
			for _, v := range p.Boundaries[b][i] {
				if v < -72 || v > 72 {
					return false
				}
			}
		}
		a, b := p.Boundaries[0][i], p.Boundaries[1][i]
		span := math.Hypot(float64(b[0]-a[0]), float64(b[1]-a[1]))
		if span < 8 || span >= 80 {
			return false
		}
	}
	return true
}

// routeIntervalOffsetsMatch admits only points between the measured endpoints;
// legacy intervals retain exact endpoints. Explicit endpoint tolerance models
// measured settling/localization error around an already reviewed clear pose.
func routeIntervalOffsetsMatch(offsets [2][2]int, p *routeInterval) bool {
	if !validRouteInterval(p) {
		return false
	}
	var progress [2]float64
	for i, point := range offsets {
		a, b := p.Boundaries[0][i], p.Boundaries[1][i]
		dx, dy := float64(b[0]-a[0]), float64(b[1]-a[1])
		ex, ey := float64(point[0]-a[0]), float64(point[1]-a[1])
		t := (ex*dx + ey*dy) / (dx*dx + dy*dy)
		if t < 0 || t > 1 {
			nearest := math.Max(0, math.Min(1, t))
			if p.EndpointTolerancePixels == 0 || math.Hypot(ex-nearest*dx, ey-nearest*dy) > p.EndpointTolerancePixels {
				return false
			}
			t = nearest
		} else if math.Hypot(ex-t*dx, ey-t*dy) > p.CrossTrackPixels {
			return false
		}
		progress[i] = t
	}
	return math.Abs(progress[0]-progress[1]) <= p.ProgressDifference
}

// routeIntervalMatches uses existing photometric landmark validation, then a
// per-route geometric gate. Missing or invalid metadata fails closed; callers
// must retain the legacy strict gate for routes without an authored interval.
func routeIntervalMatches(img image.Image, reference sceneFingerprint, p *routeInterval) bool {
	if !validRouteInterval(p) || !sceneMatches(reference, reference) {
		return false
	}
	var boxes [2]image.Rectangle
	for i, patch := range reference {
		if !patch.ArchitectureOnly {
			return false
		}
		boxes[i] = image.Rect(patch.Box[0], patch.Box[1], patch.Box[0]+patch.Box[2], patch.Box[1]+patch.Box[3])
	}
	if boxes[0].Overlaps(boxes[1]) {
		return false
	}
	horizontal, vertical := 0, 0
	for _, boundary := range p.Boundaries {
		for _, point := range boundary {
			horizontal = max(horizontal, abs(point[0]))
			vertical = max(vertical, abs(point[1]))
		}
	}
	// The existing search steps in two-pixel increments. Even bounds include
	// zero and give consistent quantization for asymmetric measured intervals.
	margin := int(math.Ceil(math.Max(p.CrossTrackPixels, p.EndpointTolerancePixels)))
	horizontal = (horizontal + margin + 1) / 2 * 2
	vertical = (vertical + margin + 1) / 2 * 2
	offsets, ok := locateRouteSceneWindow(img, reference, horizontal, vertical)
	return ok && routeIntervalOffsetsMatch(offsets, p)
}
