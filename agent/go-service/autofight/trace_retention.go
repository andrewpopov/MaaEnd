package autofight

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var traceFrameName = regexp.MustCompile(`^frame-[0-9]{6,}\.png$`)

// Only flat PNGs in completed, explicitly owned trace directories are eligible.
// Metadata, manually saved OCR evidence, symlinks and KEEP runs are preserved.
func pruneTraceImages(root string, now time.Time, maxBytes int64) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	type candidate struct {
		path     string
		size     int64
		modified time.Time
	}
	files := []candidate{}
	var total int64
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		owner, err := os.ReadFile(filepath.Join(dir, ".maaend-combat-trace"))
		if err != nil || string(owner) != "1" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "KEEP")); err == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, ".complete")); err != nil {
			continue
		}
		children, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, child := range children {
			if child.Type()&os.ModeSymlink != 0 || !traceFrameName.MatchString(child.Name()) {
				continue
			}
			info, err := child.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			total += info.Size()
			files = append(files, candidate{filepath.Join(dir, child.Name()), info.Size(), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.Before(files[j].modified) })
	for _, file := range files {
		if now.Sub(file.modified) <= 7*24*time.Hour && total <= maxBytes {
			continue
		}
		if err := os.Remove(file.path); err == nil {
			total -= file.size
		}
	}
}
