package innkeeping

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCounterShortReturnNeedsDepthCorrection(t *testing.T) {
	raw, err := os.ReadFile("testdata/inn-counter-depth-short-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference sceneFingerprint
	if err = json.Unmarshal(raw, &reference); err != nil {
		t.Fatal(err)
	}
	frame := navImage(t, "inn-counter-depth-short")
	offsets, ok := counterOriginMatch(frame, counterOriginParams{Reference: reference, Mode: "backward"})
	if !ok {
		t.Fatalf("observed short return did not authorize backward correction: %+v", offsets)
	}
	if _, ok = counterOriginMatch(frame, counterOriginParams{Reference: reference, Mode: "aligned"}); ok {
		t.Fatal("short return incorrectly authorized route departure")
	}
}
