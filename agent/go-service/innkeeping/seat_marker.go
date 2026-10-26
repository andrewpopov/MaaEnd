package innkeeping

import (
	"image"
	"math"
	"os"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Seat markers are calibrated physical number boards near the destination,
// below the HUD and outside interaction labels. Full-frame OCR misses them.
func validSeatMarker(box [4]int) bool {
	return box[0] >= 450 && box[0]+box[2] <= 1000 && box[1] >= 240 && box[1]+box[3] <= 450 && box[2] >= 16 && box[2] <= 48 && box[3] >= 24 && box[3] <= 64
}

func shiftedSeatMarker(box [4]int, offsets [2][2]int) [4]int {
	box[0] += (offsets[0][0] + offsets[1][0]) / 2
	box[1] += (offsets[0][1] + offsets[1][1]) / 2
	return box
}

func confirmsSeatMarker(tokens []screenocr.Token, seat string) bool {
	return len(tokens) == 1 && len(seat) == 1 && seat >= "1" && seat <= "6" && tokens[0].Score >= .95 && tokens[0].Text == seat
}

func readPhysicalSeatMarker(ctx *maa.Context, img image.Image, scene sceneFingerprint, marker [4]int, seat, templatePath string) bool {
	if !validSeatMarker(marker) {
		return false
	}
	if templatePath != "" {
		box, found := locatePhysicalSeatTemplate(img, templatePath, marker)
		if !found {
			return false
		}
		tokens, err := screenocr.ReadRegion(ctx, img, maa.Rect(box), true)
		return err == nil && confirmsSeatMarker(tokens, seat)
	}
	offsets, nearby := locateRouteScene(img, scene)
	if !nearby {
		return false
	}
	box := shiftedSeatMarker(marker, offsets)
	tokens, err := screenocr.ReadRegion(ctx, img, maa.Rect(box), true)
	return err == nil && confirmsSeatMarker(tokens, seat)
}

func locatePhysicalSeatTemplate(img image.Image, path string, marker [4]int) ([4]int, bool) {
	if !validSeatMarker(marker) || img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) {
		return [4]int{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return [4]int{}, false
	}
	template, _, err := image.Decode(f)
	f.Close()
	if err != nil || template.Bounds().Dx() < 16 || template.Bounds().Dx() > 48 || template.Bounds().Dy() < 24 || template.Bounds().Dy() > 64 {
		return [4]int{}, false
	}
	return matchPhysicalSeatTemplate(img, template, marker)
}

func matchPhysicalSeatTemplate(img, template image.Image, marker [4]int) ([4]int, bool) {
	box, score := physicalSeatTemplateScore(img, template, marker)
	return box, score >= .9
}

func physicalSeatTemplateScore(img, template image.Image, marker [4]int) ([4]int, float64) {
	area := image.Rect(marker[0]-128, marker[1]-40, marker[0]+marker[2]+128, marker[1]+marker[3]+40)
	crop := minicv.ImageCropRect(minicv.ImageConvertRGBA(img), area)
	tpl := minicv.ImageConvertRGBA(template)
	x, y, score, scale := minicv.MatchTemplateAnyScale(crop, minicv.GetIntegralArray(crop), tpl, .8, 1.25, []int{6, 4})
	box := [4]int{area.Min.X + int(math.Round(x)), area.Min.Y + int(math.Round(y)), int(math.Round(float64(tpl.Bounds().Dx()) * scale)), int(math.Round(float64(tpl.Bounds().Dy()) * scale))}
	return box, score
}
