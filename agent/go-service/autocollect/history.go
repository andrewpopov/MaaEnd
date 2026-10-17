// Package autocollect persists per-account route attempts and successful collections.
package autocollect

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
)

type routeHistory struct {
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	LastSuccess time.Time `json:"last_success,omitzero"`
}

type historyStore struct {
	mu     sync.Mutex
	path   string
	active map[string]time.Time
}

var history = historyStore{path: fsutil.OutputPath("data", "AutoCollect", "history.json"), active: make(map[string]time.Time)}

type historyFile struct {
	Version  int                                `json:"version"`
	Accounts map[string]map[string]routeHistory `json:"accounts"`
}

func (s *historyStore) read() (historyFile, error) {
	value := historyFile{Version: 1, Accounts: make(map[string]map[string]routeHistory)}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, err
	}
	if value.Version != 1 || value.Accounts == nil {
		return value, fmt.Errorf("unsupported AutoCollect history")
	}
	return value, nil
}

func (s *historyStore) lookup(account, route string) (routeHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.read()
	return value.Accounts[account][route], err
}

func (s *historyStore) record(account, route, event string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.read()
	if err != nil {
		return err
	}
	key := account + ":" + route
	if event == "completed" && s.active[key].IsZero() {
		return fmt.Errorf("route has no active attempt")
	}
	if value.Accounts[account] == nil {
		value.Accounts[account] = make(map[string]routeHistory)
	}
	entry := value.Accounts[account][route]
	switch event {
	case "started":
		entry.LastAttempt = now
	case "completed":
		entry.LastSuccess = now
	default:
		return fmt.Errorf("unknown route history event %q", event)
	}
	value.Accounts[account][route] = entry
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(s.path, append(data, '\n'), 0600); err != nil {
		return err
	}
	if event == "started" {
		s.active[key] = now
	} else {
		delete(s.active, key)
	}
	return nil
}

// resetBoundary uses calendar dates so 04:00 stays local across DST transitions.
func resetBoundary(now time.Time) time.Time {
	local := now.Local()
	boundary := time.Date(local.Year(), local.Month(), local.Day(), 4, 0, 0, 0, local.Location())
	if local.Before(boundary) {
		boundary = boundary.AddDate(0, 0, -1)
	}
	return boundary
}

func collectedToday(lastSuccess, now time.Time) bool {
	return !lastSuccess.IsZero() && !lastSuccess.After(now) && !lastSuccess.Before(resetBoundary(now))
}
