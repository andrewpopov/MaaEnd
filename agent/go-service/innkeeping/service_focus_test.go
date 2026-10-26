package innkeeping

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"testing"
)

func TestFocusedServicePhaseRequiresActualHeaderPosition(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token screenocr.Token
		phase string
	}{
		{"active header", screenocr.Token{Text: "End Session", Score: 1, Box: [4]int{95, 42, 120, 27}}, "active"},
		{"unrelated menu text", screenocr.Token{Text: "End Session", Score: 1, Box: [4]int{500, 400, 120, 27}}, "unknown"},
		{"uncertain text", screenocr.Token{Text: "End Session", Score: .5, Box: [4]int{95, 42, 120, 27}}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Observe([]screenocr.Token{tc.token}).Phase; got != tc.phase {
				t.Fatalf("got %s, want %s", got, tc.phase)
			}
		})
	}
	tokens := []screenocr.Token{{Text: "End Session", Score: 1, Box: [4]int{95, 42, 120, 27}}, {Text: "Closing Soon", Score: 1}}
	if got := Observe(tokens).Phase; got != "closing" {
		t.Fatalf("closing banner must override header: %s", got)
	}
}
