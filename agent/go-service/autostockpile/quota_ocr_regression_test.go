package autostockpile

import "testing"

func TestQuotaFractionWhitespace(t *testing.T) {
	for _, tc := range []struct {
		text             string
		current, maximum int
		ok               bool
	}{
		{":: Can purchase 0/960", 0, 960, true},
		{":: Can purchase 0 / 400", 0, 400, true},
		{"Can purchase 125 / 960", 125, 960, true},
		{"Can purchase unavailable", 0, 0, false},
	} {
		current, maximum, ok := parseOverflowCurrentMax([]string{tc.text})
		if current != tc.current || maximum != tc.maximum || ok != tc.ok {
			t.Errorf("%q: got %d/%d %v", tc.text, current, maximum, ok)
		}
	}
}

func TestUnavailableQuotaFailsTask(t *testing.T) {
	if !shouldStopTask(AbortReasonQuotaUnavailableWarn) {
		t.Fatal("unreadable quota must not report a successful skip")
	}
	if shouldStopTask(AbortReasonQuotaZeroSkip) {
		t.Fatal("exhausted quota is a normal skip")
	}
}
