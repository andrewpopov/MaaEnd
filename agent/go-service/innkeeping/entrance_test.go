package innkeeping

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

func TestSetupTravelRequiresPositiveNonSessionScreen(t *testing.T) {
	token := func(text string) screenocr.Token {
		return screenocr.Token{Text: text, Score: .99, Box: [4]int{30, 20, 180, 30}}
	}
	for _, tc := range []struct {
		name   string
		tokens []screenocr.Token
		want   bool
	}{
		{"gear menu", []screenocr.Token{token("Equip")}, true},
		{"world", []screenocr.Token{token("Explore")}, true},
		{"unknown", nil, false},
		{"Si dialogue", []screenocr.Token{token("Si")}, false},
		{"quest alone", []screenocr.Token{token("Yuanyuan Innkeeper III")}, false},
		{"preparation", []screenocr.Token{token("Explore"), token("INN SETTINGS")}, false},
		{"tutorial", []screenocr.Token{token("Explore"), token("Innkeeping: Gourmand")}, false},
		{"active", []screenocr.Token{token("Explore"), token("Current Income"), token("Patrons Seated"), token("Dishes Sold")}, false},
		{"closing", []screenocr.Token{token("Explore"), token("Closing Soon")}, false},
		{"closed", []screenocr.Token{token("Explore"), token("Inn Closed")}, false},
		{"result", []screenocr.Token{token("Explore"), token("Session Ended")}, false},
		{"kitchen", []screenocr.Token{token("Explore"), token("Kitchen 1"), token("Kitchen 2")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := setupTravelScreen(tc.tokens); got != tc.want {
				t.Fatalf("travel permitted=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestSetupTravelAcceptsVerifiedMapWithoutExploreHUD(t *testing.T) {
	tokens := []screenocr.Token{{Text: "Marker Tags", Score: .99, Box: [4]int{50, 635, 150, 30}}, {Text: "Region Overview", Score: .99, Box: [4]int{1000, 635, 210, 30}}}
	if !setupTravelScreen(tokens) {
		t.Fatal("map cannot dispatch travel to innkeeper")
	}
	if setupTravelScreen(tokens[:1]) {
		t.Fatal("single incidental map label authorized travel")
	}
	tokens = append(tokens, screenocr.Token{Text: "Kitchen 1", Score: .99}, screenocr.Token{Text: "Kitchen 2", Score: .99})
	if setupTravelScreen(tokens) {
		t.Fatal("active kitchen must not dispatch teleport")
	}
}

func TestSharedMapTravelEvidenceDoesNotOverrideInnScreens(t *testing.T) {
	if !setupTravelPermitted(nil, true) || setupTravelPermitted(nil, false) {
		t.Fatal("native map evidence was not required")
	}
	for _, text := range []string{"INN SETTINGS", "Kitchen 1", "Session Ended", "Closing Soon"} {
		tokens := []screenocr.Token{{Text: text, Score: .99}}
		if text == "Kitchen 1" {
			tokens = append(tokens, screenocr.Token{Text: "Kitchen 2", Score: .99})
		}
		if setupTravelPermitted(tokens, true) {
			t.Fatalf("map evidence overrode inn screen %q", text)
		}
	}
}
