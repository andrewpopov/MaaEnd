package innkeeping

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// TimingObservation identifies a stable diner or kitchen job across successive frames.
type TimingObservation struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Phase   string `json:"phase"`
	Seat    string `json:"seat,omitempty"`
	Kitchen string `json:"kitchen,omitempty"`
	Chef    string `json:"chef,omitempty"`
	Dish    string `json:"dish,omitempty"`
}

// TimingEvent keeps the observation interval rather than inventing an exact duration.
type TimingEvent struct {
	At         time.Time `json:"at"`
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Seat       string    `json:"seat,omitempty"`
	Kitchen    string    `json:"kitchen,omitempty"`
	Chef       string    `json:"chef,omitempty"`
	Dish       string    `json:"dish,omitempty"`
	SecondsMin float64   `json:"seconds_min"`
	SecondsMax float64   `json:"seconds_max"`
	Measured   bool      `json:"measured"`
}

type timingTrack struct {
	Observation TimingObservation
	Last        time.Time
	EnterAfter  time.Time
	EnterBefore time.Time
	ExactStart  bool
}

// TimingTracker handles each diner visit and cooking job independently. Missing data
// never means finished: only an observed phase transition creates a duration sample.
type TimingTracker struct {
	tracks    map[string]timingTrack
	lastFrame time.Time
}

// Observe emits changes once, tolerates missed frames, and rejects time going backwards.
func (t *TimingTracker) Observe(at time.Time, observations []TimingObservation) ([]TimingEvent, error) {
	if !t.lastFrame.IsZero() && at.Before(t.lastFrame) {
		return nil, fmt.Errorf("observation time moved backwards")
	}
	if t.tracks == nil {
		t.tracks = map[string]timingTrack{}
	}
	events := []TimingEvent{}
	seen := map[string]bool{}
	for _, o := range observations {
		if o.ID == "" || o.Kind == "" || o.Phase == "" {
			return nil, fmt.Errorf("timing observation requires identity, kind and phase")
		}
		key := o.Kind + ":" + o.ID
		if seen[key] {
			return nil, fmt.Errorf("duplicate observation %s", key)
		}
		seen[key] = true
		old, exists := t.tracks[key]
		if !exists {
			t.tracks[key] = timingTrack{Observation: o, Last: at, EnterBefore: at}
			events = append(events, TimingEvent{At: at, ID: o.ID, Kind: o.Kind, To: o.Phase, Seat: o.Seat, Kitchen: o.Kitchen, Chef: o.Chef, Dish: o.Dish})
			continue
		}
		if old.Observation.Phase == o.Phase {
			old.Last = at
			old.Observation = o
			t.tracks[key] = old
			continue
		}
		complete := (old.Observation.Phase == "cooking" && o.Phase == "ready") || (old.Observation.Phase == "dining" && o.Phase == "bill_ready")
		event := TimingEvent{At: at, ID: o.ID, Kind: o.Kind, From: old.Observation.Phase, To: o.Phase, Seat: o.Seat, Kitchen: o.Kitchen, Chef: o.Chef, Dish: o.Dish, Measured: complete && old.ExactStart}
		if event.Measured {
			event.SecondsMin = old.Last.Sub(old.EnterBefore).Seconds()
			event.SecondsMax = at.Sub(old.EnterAfter).Seconds()
		}
		events = append(events, event)
		t.tracks[key] = timingTrack{Observation: o, Last: at, EnterAfter: old.Last, EnterBefore: at, ExactStart: true}
	}
	t.lastFrame = at
	return events, nil
}

// AppendEvents stores durable, replayable records. Screenshot paths can be attached
// by the caller; images stay in the user's configured capture directory.
func AppendEvents(path string, events []TimingEvent) error {
	if len(events) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return f.Sync()
}

// TimingSummary groups empirical durations by kitchen, cook and recipe or diner class.
func TimingSummary(events []TimingEvent) map[string][]float64 {
	result := map[string][]float64{}
	for _, e := range events {
		if !e.Measured {
			continue
		}
		key := e.Kind + "/" + e.Kitchen + "/" + e.Chef + "/" + e.Dish
		result[key] = append(result[key], (e.SecondsMin+e.SecondsMax)/2)
	}
	for _, samples := range result {
		sort.Float64s(samples)
	}
	return result
}
