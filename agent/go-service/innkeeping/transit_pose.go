package innkeeping

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"image"
	"os"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// A transit pose is independent of the destination order. A nearby Table 1
// board can, for example, locate the counter's outbound camera pose.
type transitPose struct {
	Number   string `json:"number"`
	Box      [4]int `json:"box"`
	Template string `json:"template"`
}

func validTransitPose(p *transitPose) bool {
	return p != nil && len(p.Number) == 1 && p.Number >= "1" && p.Number <= "6" && validSeatMarker(p.Box) && p.Template != ""
}

func transitPoseMatches(ctx *maa.Context, img image.Image, scene sceneFingerprint, p *transitPose) bool {
	if p == nil {
		return sceneMatchesImage(img, scene)
	}
	box, fallback, ok := transitPoseLocation(img, scene, p)
	if !ok {
		return false
	}
	tokens, err := screenocr.ReadRegion(ctx, img, maa.Rect(box), true)
	return err == nil && confirmsSeatMarker(tokens, p.Number) && (!fallback || tokens[0].Score >= .98)
}

func transitPoseGeometry(img image.Image, scene sceneFingerprint, p *transitPose) ([4]int, bool) {
	box, _, ok := transitPoseLocation(img, scene, p)
	return box, ok
}

func transitPoseLocation(img image.Image, scene sceneFingerprint, p *transitPose) ([4]int, bool, bool) {
	if !validTransitPose(p) {
		return [4]int{}, false, false
	}
	box, found := locatePhysicalSeatTemplate(img, p.Template, p.Box)
	bounded := abs(box[0]-p.Box[0]) <= 48 && abs(box[1]-p.Box[1]) <= 40
	if found && bounded && sceneMatchesImage(img, scene) {
		return box, false, true
	}
	// A replay reached the same numbered table with about 28px vertical
	// variation. Only a verified board permits this larger architectural
	// search, and both landmarks must agree on the view's displacement.
	offsets, ok := locateRouteSceneWindow(img, scene, 48, 48)
	if !ok || abs(offsets[0][0]-offsets[1][0]) > 12 || abs(offsets[0][1]-offsets[1][1]) > 12 {
		return box, false, false
	}
	if found && bounded {
		return box, false, true
	}
	box = shiftedSeatMarker(p.Box, offsets)
	if abs(box[0]-p.Box[0]) > 48 || abs(box[1]-p.Box[1]) > 40 {
		return box, false, false
	}
	r := image.Rect(box[0], box[1], box[0]+box[2], box[1]+box[3])
	if img == nil || !r.In(img.Bounds()) {
		return box, false, false
	}
	f, err := os.Open(p.Template)
	if err != nil {
		return box, false, false
	}
	reference, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return box, false, false
	}
	current := image.NewRGBA(image.Rect(0, 0, box[2], box[3]))
	for y := 0; y < box[3]; y++ {
		for x := 0; x < box[2]; x++ {
			current.Set(x, y, img.At(box[0]+x, box[1]+y))
		}
	}
	return box, true, placardMatches(current, reference)
}
