package innkeeping

import (
	"encoding/json"
	"image"
	"os"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomRecognitionRunner = &PhysicalBoardRecognition{}

// PhysicalBoardRecognition re-localizes an observed plaque on the native fresh
// image, then independently reads its number. It never grants route replay credit.
type PhysicalBoardRecognition struct{}

func validBoardProbe(p transitPose) bool {
	b := p.Box
	return len(p.Number) == 1 && p.Number >= "1" && p.Number <= "6" && p.Template != "" &&
		b[0] >= 180 && b[0]+b[2] <= 1280 && b[1] >= 240 && b[1]+b[3] <= 500 &&
		b[2] >= 8 && b[2] <= 85 && b[3] >= 16 && b[3] <= 90
}

func (r *PhysicalBoardRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	var p transitPose
	if json.Unmarshal([]byte(arg.CustomRecognitionParam), &p) != nil || !validBoardProbe(p) {
		return nil, false
	}
	f, err := os.Open(p.Template)
	if err != nil {
		return nil, false
	}
	template, _, err := image.Decode(f)
	f.Close()
	if err != nil || template.Bounds().Dx() != p.Box[2] || template.Bounds().Dy() != p.Box[3] {
		return nil, false
	}
	box, score := physicalSeatTemplateScore(arg.Img, template, p.Box)
	if score < .9 || abs(box[0]-p.Box[0]) > 64 || abs(box[1]-p.Box[1]) > 40 {
		return nil, false
	}
	tokens, err := screenocr.ReadRegion(ctx, arg.Img, maa.Rect(box), true)
	if err != nil || !boardIdentity(p.Number, score, tokens) {
		return nil, false
	}
	detail, _ := json.Marshal(map[string]any{"number": p.Number, "box": box, "template_score": score, "ocr_confirmed": confirmsSeatMarker(tokens, p.Number)})
	return &maa.CustomRecognitionResult{Box: maa.Rect(box), Detail: string(detail)}, true
}

// A strong fresh match to an independently read digit remains useful when OCR
// loses confidence on a 20-pixel sign. Confident contradictory digits stop it.
func boardIdentity(number string, score float64, tokens []screenocr.Token) bool {
	if score < .9 {
		return false
	}
	for _, token := range tokens {
		if token.Score >= .95 && len(token.Text) == 1 && token.Text >= "1" && token.Text <= "6" && token.Text != number {
			return false
		}
	}
	return confirmsSeatMarker(tokens, number) || score >= .95
}
