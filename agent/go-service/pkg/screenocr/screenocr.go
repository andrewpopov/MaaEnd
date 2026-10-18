// Package screenocr reads text from a supplied screenshot without sending input.
package screenocr

import (
	"encoding/json"
	"fmt"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"image"
)

// Token preserves text, confidence and its 720p bounding box.
type Token struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
	Box   [4]int  `json:"box"`
}

// Read performs full-frame OCR using the callback's existing image and context.
func Read(ctx *maa.Context, img image.Image) ([]Token, error) {
	if ctx == nil || img == nil || img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 720 {
		return nil, fmt.Errorf("OCR requires a 1280x720 screenshot")
	}
	return ReadRegion(ctx, img, maa.Rect{0, 0, 1280, 720}, false)
}

// ReadRegion recognizes an observed fixed text field, optionally without detection.
func ReadRegion(ctx *maa.Context, img image.Image, roi maa.Rect, onlyRec bool) ([]Token, error) {
	param := maa.OCRParam{ROI: maa.NewTargetRect(roi), OnlyRec: onlyRec}
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, &param, img)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, fmt.Errorf("OCR returned no recognition detail")
	}
	var raw struct {
		All []Token `json:"all"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &raw); err != nil {
		return nil, err
	}
	return raw.All, nil
}
