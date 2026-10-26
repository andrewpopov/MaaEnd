package autofight

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCombatTraceOptInAndFlush(t *testing.T) {
	t.Setenv("MAAEND_COMBAT_TRACE_DIR", "")
	if newCombatTrace() != nil {
		t.Fatal("trace should be disabled by default")
	}
	root := filepath.Join(t.TempDir(), "new", "trace-parent")
	t.Setenv("MAAEND_COMBAT_TRACE_DIR", root)
	trace := newCombatTrace()
	if trace == nil {
		t.Fatal("failed to start trace")
	}
	trace.frame(image.NewRGBA(image.Rect(0, 0, 1280, 720)), &ScreenAnalyzer{}, time.Now())
	trace.close()
	files, err := filepath.Glob(filepath.Join(root, "combat-*", "trace.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatal("trace missing")
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	foundFrame, foundStop := false, false
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row["type"] == "frame" {
			foundFrame = true
			if _, err := os.Stat(filepath.Join(filepath.Dir(files[0]), row["image"].(string))); err != nil {
				t.Fatal(err)
			}
		}
		if row["type"] == "trace_stop" {
			foundStop = true
		}
	}
	if !foundFrame || !foundStop {
		t.Fatal("frame or final integrity record missing")
	}
}
