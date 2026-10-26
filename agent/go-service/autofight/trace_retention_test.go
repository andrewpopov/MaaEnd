package autofight

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTraceRetentionPreservesEvidenceAndKeepRuns(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	makeRun := func(name string, owned, complete, keep bool) string {
		dir := filepath.Join(root, name)
		os.Mkdir(dir, 0700)
		if owned {
			os.WriteFile(filepath.Join(dir, ".maaend-combat-trace"), []byte("1"), 0600)
		}
		if complete {
			os.WriteFile(filepath.Join(dir, ".complete"), nil, 0600)
		}
		if keep {
			os.WriteFile(filepath.Join(dir, "KEEP"), nil, 0600)
		}
		os.WriteFile(filepath.Join(dir, "trace.jsonl"), []byte("metadata"), 0600)
		frame := filepath.Join(dir, "frame-000001.png")
		os.WriteFile(frame, []byte("image"), 0600)
		os.Chtimes(frame, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour))
		return dir
	}
	expired := makeRun("expired", true, true, false)
	keep := makeRun("keep", true, true, true)
	active := makeRun("active", true, false, false)
	evidence := makeRun("evidence", false, true, false)
	pruneTraceImages(root, now, 1024)
	if _, err := os.Stat(filepath.Join(expired, "frame-000001.png")); !os.IsNotExist(err) {
		t.Fatal("expired image not removed")
	}
	for _, dir := range []string{keep, active, evidence} {
		if _, err := os.Stat(filepath.Join(dir, "frame-000001.png")); err != nil {
			t.Fatal("protected image removed")
		}
	}
	if _, err := os.Stat(filepath.Join(expired, "trace.jsonl")); err != nil {
		t.Fatal("metadata removed")
	}
}
func TestTraceRetentionSizeBudget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run")
	os.Mkdir(dir, 0700)
	os.WriteFile(filepath.Join(dir, ".maaend-combat-trace"), []byte("1"), 0600)
	os.WriteFile(filepath.Join(dir, ".complete"), nil, 0600)
	os.WriteFile(filepath.Join(dir, "frame-000001.png"), []byte("12345"), 0600)
	pruneTraceImages(root, time.Now(), 0)
	if _, err := os.Stat(filepath.Join(dir, "frame-000001.png")); !os.IsNotExist(err) {
		t.Fatal("over-budget image not removed")
	}
}
