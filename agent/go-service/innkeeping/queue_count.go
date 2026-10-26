package innkeeping

import (
	"encoding/json"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"sync"
)

var _ maa.CustomRecognitionRunner = &KitchenQueueCountRecognition{}

// KitchenQueueCountRecognition acknowledges visible queued dishes before the
// next recipe click. The selected kitchen and all three queue slots must be known.
type KitchenQueueCountRecognition struct {
	once  sync.Once
	icons IconLibrary
	err   error
}

func queueCountMatches(v LiveView, kitchen string, count int) bool {
	if count < 0 || count > 3 || (kitchen != "1" && kitchen != "2") || v.Phase != "kitchen" || v.Kitchen != kitchen || !v.QueueVerified || len(v.Unknown) > 0 {
		return false
	}
	total := 0
	for _, n := range v.Queue {
		total += n
	}
	return total == count
}

// Exact dish counts prevent a completed dish plus an incorrect new selection
// from accidentally satisfying a total-only queue acknowledgement.
func queueContentsMatch(v LiveView, expected map[string]int) bool {
	if expected == nil {
		return true
	}
	total := 0
	for dish, count := range expected {
		if dish == "" || count < 0 || count > 3 || v.Queue[dish] != count {
			return false
		}
		total += count
	}
	if total > 3 {
		return false
	}
	for dish, count := range v.Queue {
		if expected[dish] != count {
			return false
		}
	}
	return true
}

func (r *KitchenQueueCountRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	var p struct {
		Kitchen  string         `json:"kitchen"`
		Count    int            `json:"count"`
		Expected map[string]int `json:"expected"`
	}
	if json.Unmarshal([]byte(arg.CustomRecognitionParam), &p) != nil || p.Count < 0 || p.Count > 3 {
		return nil, false
	}
	r.once.Do(func() { r.icons, r.err = LoadIcons(fsutil.OutputPath("data", "Innkeeping", "icons")) })
	if r.err != nil {
		return nil, false
	}
	tokens, err := screenocr.ReadRegion(ctx, arg.Img, maa.Rect{40, 95, 600, 65}, false)
	if err != nil || Observe(tokens).Phase != "kitchen" {
		return nil, false
	}
	v := ReadLive(arg.Img, tokens, r.icons, nil)
	if !queueCountMatches(v, p.Kitchen, p.Count) || !queueContentsMatch(v, p.Expected) {
		return nil, false
	}
	detail, _ := json.Marshal(map[string]any{"kitchen": v.Kitchen, "queue": v.Queue})
	return &maa.CustomRecognitionResult{Box: maa.Rect{100, 530, 320, 120}, Detail: string(detail)}, true
}
