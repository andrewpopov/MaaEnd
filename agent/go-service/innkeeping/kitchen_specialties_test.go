package innkeeping

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

func TestKitchenSpecialtiesFromObservedPanels(t *testing.T) {
	for _, tc := range []struct {
		frame string
		want  []string
	}{
		{"inn-live-k1", []string{"entree", "dessert"}},
		{"inn-final-k2", []string{"soup", "side"}},
	} {
		t.Run(tc.frame, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + tc.frame + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var tokens []screenocr.Token
			if err = json.Unmarshal(data, &tokens); err != nil {
				t.Fatal(err)
			}
			got := kitchenSpecialties(tokens)
			if len(got) != len(tc.want) {
				t.Fatalf("bonuses: %v, want %v", got, tc.want)
			}
			for _, category := range tc.want {
				if !includes(got, category) {
					t.Fatalf("missing %s in %v", category, got)
				}
			}
		})
	}
}

func TestKitchenSpecialtiesDoNotLeakAcrossThemes(t *testing.T) {
	tokens := []screenocr.Token{
		{Text: "Entree", Score: .99, Box: [4]int{133, 410, 90, 22}},
		{Text: "Soup", Score: .99, Box: [4]int{600, 230, 90, 22}},
		{Text: "Dessert", Score: .50, Box: [4]int{273, 410, 90, 22}},
		{Text: "Savory", Score: .99, Box: [4]int{110, 435, 90, 22}},
	}
	got := kitchenSpecialties(tokens)
	if len(got) != 1 || got[0] != "entree" {
		t.Fatalf("unobserved specialty inferred: %v", got)
	}
	if got := kitchenSpecialties(nil); len(got) != 0 {
		t.Fatalf("unknown panel inherited bonuses: %v", got)
	}
}
