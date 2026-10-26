package innkeeping

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

func TestEndedObservationPreservesScreenAndRawOCRWithoutInferringSuccess(t *testing.T) {
	root := t.TempDir()
	s := &liveSession{ID: "saved-trial", CaptureRoot: root}
	tokens := []screenocr.Token{{Text: "Session Ended", Score: .99, Box: [4]int{500, 200, 250, 40}}}
	if err := s.archiveEndedObservation(123, time.Unix(1000, 0), navImage(t, "inn-trial-ended"), tokens); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*-ended-observation-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal("ended screen lost its paired OCR record", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var record endedObservation
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.RunMode != "unknown" || record.TaskID != 123 || !reflect.DeepEqual(record.OCR, tokens) || strings.TrimSuffix(record.Screenshot, ".png")+".json" != files[0] {
		t.Fatal("screen evidence acquired an unsupported run type or lost raw OCR", record)
	}
	if info, err := os.Stat(record.Screenshot); err != nil || info.Size() == 0 {
		t.Fatal("ended-screen image was not persisted before dismissal", err)
	}
}
