package innkeeping

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestFocusedTimesRepairPlanningAndInputView(t *testing.T) {
	// Actual failed frame: full OCR produced clock "0254" and badge "311s".
	// Measured isolated fields produced 02:54 (.9595) and 11s (.9973).
	v := LiveView{Phase: "kitchen", Kitchen: "1", QueueVerified: true,
		Recipes: []RecipeView{{Dish: "tonic_congee_greens", Available: true, Box: [4]int{596, 307, 150, 55}}},
		Seats:   []SeatView{{Dishes: []string{"tonic_congee_greens"}}}}
	catalog := []DishDefinition{{ID: "tonic_congee_greens", BaseCookSeconds: 15}}
	v = focusedLiveTimes(v, catalog, func(region maa.Rect) ([]screenocr.Token, error) {
		switch region {
		case maa.Rect{95, 18, 68, 28}:
			return []screenocr.Token{{Text: "02:54", Score: .959503}}, nil
		case maa.Rect{478, 288, 34, 27}:
			return []screenocr.Token{{Text: "11s", Score: .99734}}, nil
		default:
			t.Fatalf("unmeasured timer region: %v", region)
			return nil, nil
		}
	})
	if v.RemainingSeconds != 174 || v.Recipes[0].Seconds != 11 {
		t.Fatalf("focused read did not repair both fields: %+v", v)
	}
	if _, ok := eligibleInput(v, pendingInput{Kind: "queue", Kitchen: "1", Dish: "tonic_congee_greens"}); !ok {
		t.Fatal("fresh input check must retain the same verified clock and cooking time")
	}
}

func TestFocusedTimesRejectAmbiguousAndImpossibleValues(t *testing.T) {
	for _, text := range []string{"0254", "02:99", "09:54"} {
		v := focusedLiveTimes(LiveView{Phase: "active"}, nil, func(maa.Rect) ([]screenocr.Token, error) {
			return []screenocr.Token{{Text: text, Score: .99}}, nil
		})
		if v.RemainingSeconds != 0 {
			t.Fatalf("unsafe clock %q became %d seconds", text, v.RemainingSeconds)
		}
	}
}

func TestFocusedClockUsesPhaseSpecificRegion(t *testing.T) {
	for phase, expected := range map[string]maa.Rect{"active": {96, 92, 70, 30}, "kitchen": {95, 18, 68, 28}} {
		v := focusedLiveTimes(LiveView{Phase: phase}, nil, func(region maa.Rect) ([]screenocr.Token, error) {
			if region != expected {
				t.Fatalf("%s clock region = %v, want %v", phase, region, expected)
			}
			return []screenocr.Token{{Text: "02:55", Score: .99}}, nil
		})
		if v.RemainingSeconds != 175 {
			t.Fatalf("%s clock = %d", phase, v.RemainingSeconds)
		}
	}
}
