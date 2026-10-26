package innkeeping

import (
	"encoding/json"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &ReadyFoodOffsetRecognition{}

// ReadyFoodOffsetRecognition localizes a known ready dish along the counter.
// Pipeline verifies the active HUD and owns each bounded lateral input.
type ReadyFoodOffsetRecognition struct {
	once  sync.Once
	icons IconLibrary
	err   error
}

func readyFoodOffset(goals []WorldFood, dish, side string) (WorldFood, bool) {
	if side != "left" && side != "right" && side != "center" {
		return WorldFood{}, false
	}
	for _, g := range goals {
		center := g.Box[0] + g.Box[2]/2
		if g.Counter && g.Dish == dish && ((side == "left" && center < 600) || (side == "right" && center > 700) || (side == "center" && center >= 500 && center <= 780)) {
			return g, true
		}
	}
	return WorldFood{}, false
}

// Run reads only the supplied fresh image; no controller or input is acquired.
func (r *ReadyFoodOffsetRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}
	var param struct {
		Dish string `json:"dish"`
		Side string `json:"side"`
	}
	if json.Unmarshal([]byte(arg.CustomRecognitionParam), &param) != nil || param.Dish == "" || (param.Side != "left" && param.Side != "right" && param.Side != "center") {
		return nil, false
	}
	r.once.Do(func() { r.icons, r.err = LoadIcons(fsutil.OutputPath("data", "Innkeeping", "icons")) })
	if r.err != nil {
		log.Error().Err(r.err).Str("component", "InnkeepingReadyFoodOffset").Msg("Load ready-food icons")
		return nil, false
	}
	g, ok := readyFoodOffset(WorldFoods(arg.Img, r.icons, true), param.Dish, param.Side)
	if !ok {
		return nil, false
	}
	data, _ := json.Marshal(g)
	return &maa.CustomRecognitionResult{Box: maa.Rect(g.Box), Detail: string(data)}, true
}
