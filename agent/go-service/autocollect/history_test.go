package autocollect

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCooldownUsesSuccessfulCompletionAndLocalReset(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("test", -7*60*60)
	defer func() { time.Local = previous }()
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, time.Local) }
	cases := []struct {
		name         string
		success, now time.Time
		want         bool
	}{
		{"never completed", time.Time{}, at(14, 12, 0), false},
		{"same game day", at(13, 22, 0), at(14, 3, 59), true},
		{"exact reset", at(13, 22, 0), at(14, 4, 0), false},
		{"completion at reset", at(14, 4, 0), at(14, 4, 1), true},
		{"future clock", at(15, 8, 0), at(14, 8, 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := collectedToday(c.success, c.now); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestHistoryPersistsAndSeparatesAccountsAndRoutes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	s := historyStore{path: path, active: make(map[string]time.Time)}
	now := time.Now()
	if err := s.record("account-a", "AutoCollectCommonRoute1", "started", now); err != nil {
		t.Fatal(err)
	}
	reloaded := historyStore{path: path, active: make(map[string]time.Time)}
	attempted, err := reloaded.lookup("account-a", "AutoCollectCommonRoute1")
	if err != nil || !attempted.LastSuccess.IsZero() || !attempted.LastAttempt.Equal(now) {
		t.Fatalf("interrupted attempt must not complete: %+v %v", attempted, err)
	}
	if err := reloaded.record("account-a", "AutoCollectCommonRoute1", "completed", now); err == nil {
		t.Fatal("restarted process must not complete an old attempt")
	}
	if err := s.record("account-a", "AutoCollectCommonRoute1", "completed", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	done, err := reloaded.lookup("account-a", "AutoCollectCommonRoute1")
	if err != nil || !done.LastSuccess.Equal(now.Add(time.Minute)) {
		t.Fatalf("lost completion: %+v %v", done, err)
	}
	for _, key := range [][2]string{{"account-b", "AutoCollectCommonRoute1"}, {"account-a", "AutoCollectCommonRoute2"}} {
		other, err := reloaded.lookup(key[0], key[1])
		if err != nil || !other.LastSuccess.IsZero() {
			t.Fatalf("cross-account/route cooldown: %+v %v", other, err)
		}
	}
}

func TestCorruptHistoryIsNotSilentlyOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	s := historyStore{path: path, active: make(map[string]time.Time)}
	if _, err := s.lookup("a", "AutoCollectCommonRoute1"); err == nil {
		t.Fatal("corrupt history accepted")
	}
	if err := s.record("a", "AutoCollectCommonRoute1", "started", time.Now()); err == nil {
		t.Fatal("corrupt history overwritten")
	}
}

func TestResetBoundaryAcrossDaylightSaving(t *testing.T) {
	zone, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = zone
	defer func() { time.Local = previous }()
	now := time.Date(2026, 3, 8, 3, 30, 0, 0, zone)
	want := time.Date(2026, 3, 7, 4, 0, 0, 0, zone)
	if got := resetBoundary(now); !got.Equal(want) {
		t.Fatalf("DST boundary: %v want %v", got, want)
	}
}
