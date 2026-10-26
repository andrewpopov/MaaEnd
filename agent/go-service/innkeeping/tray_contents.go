package innkeeping

import (
	"encoding/json"
	"image"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomRecognitionRunner = &TrayContentsRecognition{}

// TrayContentsRecognition acknowledges inventory transitions without OCR.
// The Pipeline independently guards the active service phase on the same frame.
type TrayContentsRecognition struct {
	once  sync.Once
	icons IconLibrary
	err   error
}

func trayContentsMatch(img image.Image, lib IconLibrary, expected map[string]int) bool {
	if img == nil || img.Bounds() != image.Rect(0, 0, 1280, 720) || expected == nil {
		return false
	}
	total := 0
	for dish, count := range expected {
		if dish == "" || count < 1 || count > 5 {
			return false
		}
		total += count
	}
	if total > 5 {
		return false
	}
	observed := map[string]int{}
	for slot := 0; slot < 5; slot++ {
		x := 483 + 79*slot
		got, _ := lib.Identify(img, x, 623, 28)
		if got == "" {
			if colorful(img, x, 623, 20) {
				return false
			}
		} else {
			observed[got]++
		}
	}
	if len(observed) != len(expected) {
		return false
	}
	for dish, count := range expected {
		if observed[dish] != count {
			return false
		}
	}
	return true
}

func (r *TrayContentsRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	var p struct {
		Expected map[string]int `json:"expected"`
	}
	if json.Unmarshal([]byte(arg.CustomRecognitionParam), &p) != nil || p.Expected == nil {
		return nil, false
	}
	r.once.Do(func() { r.icons, r.err = LoadIcons(fsutil.OutputPath("data", "Innkeeping", "icons")) })
	if r.err != nil || !trayContentsMatch(arg.Img, r.icons, p.Expected) {
		return nil, false
	}
	detail, _ := json.Marshal(map[string]any{"tray": p.Expected})
	return &maa.CustomRecognitionResult{Box: maa.Rect{445, 590, 395, 70}, Detail: string(detail)}, true
}
