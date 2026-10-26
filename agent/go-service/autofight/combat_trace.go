package autofight

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

type traceItem struct {
	record map[string]any
	img    image.Image
}
type combatTrace struct {
	root    string
	queue   chan traceItem
	done    chan struct{}
	start   time.Time
	seq     uint64
	dropped atomic.Uint64
	failed  atomic.Uint64
	bytes   int64
}

// Opt-in tracing consumes the exact analyzer image without another screenshot.
// Bounded buffers and a storage cap prevent instrumentation from stalling combat.
func newCombatTrace() *combatTrace {
	root := os.Getenv("MAAEND_COMBAT_TRACE_DIR")
	if root == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil
	}
	pruneTraceImages(root, time.Now(), 1024*1024*1024)
	dir, err := os.MkdirTemp(root, "combat-")
	if err != nil {
		return nil
	}
	if err := os.WriteFile(filepath.Join(dir, ".maaend-combat-trace"), []byte("1"), 0600); err != nil {
		return nil
	}
	file, err := os.Create(filepath.Join(dir, "trace.jsonl"))
	if err != nil {
		return nil
	}
	t := &combatTrace{root: root, queue: make(chan traceItem, 4), done: make(chan struct{}), start: time.Now()}
	go func() {
		defer close(t.done)
		defer file.Close()
		enc := json.NewEncoder(file)
		for item := range t.queue {
			if item.img != nil && t.bytes < 256*1024*1024 {
				name := fmt.Sprintf("frame-%06d.png", item.record["frame_id"])
				path := filepath.Join(dir, name)
				f, e := os.Create(path)
				if e == nil {
					e = png.Encode(f, item.img)
					if ce := f.Close(); e == nil {
						e = ce
					}
				}
				if e == nil {
					item.record["image"] = name
					if stat, e := os.Stat(path); e == nil {
						t.bytes += stat.Size()
					}
				} else {
					t.failed.Add(1)
				}
			}
			if e := enc.Encode(item.record); e != nil {
				t.failed.Add(1)
			}
		}
		if err := enc.Encode(map[string]any{"type": "trace_stop", "dropped_records": t.dropped.Load(), "write_errors": t.failed.Load(), "image_bytes": t.bytes}); err == nil && t.failed.Load() == 0 {
			os.WriteFile(filepath.Join(dir, ".complete"), []byte("1"), 0600)
		}
	}()
	t.event("trace_start", map[string]any{"started_utc": t.start.UTC().Format(time.RFC3339Nano), "input_source": "existing_AutoFight", "storage_limit_bytes": 256 * 1024 * 1024})
	return t
}

func (t *combatTrace) event(kind string, fields map[string]any) {
	if t == nil {
		return
	}
	fields["type"] = kind
	fields["elapsed_ms"] = time.Since(t.start).Seconds() * 1000
	fields["frame_id"] = t.seq
	select {
	case t.queue <- traceItem{record: fields}:
	default:
		t.dropped.Add(1)
	}
}
func (t *combatTrace) frame(img image.Image, sa *ScreenAnalyzer, started time.Time) {
	if t == nil {
		return
	}
	t.seq++
	if len(t.queue) == cap(t.queue) {
		t.dropped.Add(1)
		return
	}
	clone := image.NewRGBA(img.Bounds())
	draw.Draw(clone, clone.Bounds(), img, img.Bounds().Min, draw.Src)
	detections := []screenDetection{}
	if len(sa.frames) > 0 {
		detections = append(detections, sa.frames[len(sa.frames)-1].Detections...)
	}
	record := map[string]any{"type": "frame", "frame_id": t.seq, "elapsed_ms": time.Since(t.start).Seconds() * 1000,
		"capture_started_ms": started.Sub(t.start).Seconds() * 1000, "capture_and_detection_ms": time.Since(started).Seconds() * 1000,
		"detections": detections, "mobile_layout": sa.mobileLayout}
	select {
	case t.queue <- traceItem{record: record, img: clone}:
	default:
		t.dropped.Add(1)
	}
}
func (t *combatTrace) close() {
	if t != nil {
		close(t.queue)
		<-t.done
		pruneTraceImages(t.root, time.Now(), 1024*1024*1024)
	}
}
