package innkeeping

import (
	"encoding/json"
	"image"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

type endedObservation struct {
	Session    string            `json:"session"`
	TaskID     int64             `json:"task_id"`
	At         time.Time         `json:"at"`
	RunMode    string            `json:"run_mode"`
	Screenshot string            `json:"screenshot"`
	OCR        []screenocr.Token `json:"ocr"`
}

// Preserve the current ended screen before Continue can dismiss it. A result
// image alone cannot distinguish a Trial from a real run, so mode stays unknown.
// An ended notice also does not by itself establish a score or star rating.
func (s *liveSession) archiveEndedObservation(taskID int64, at time.Time, img image.Image, tokens []screenocr.Token) error {
	path, err := s.captureEvidence(at, img, "ended-observation")
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(endedObservation{s.ID, taskID, at, "unknown", path, tokens}, "", "    ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(strings.TrimSuffix(path, ".png")+".json", data, 0644)
}
