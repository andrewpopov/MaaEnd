package creditshopping

import "testing"

func TestObservedWrappedEnglishItemNames(t *testing.T) {
	for text, want := range map[string]string{
		"Elementary Cognitive":         "ElementaryCognitiveCarrier",
		"Elementary Combat":            "ElementaryCombatRecord",
		"Intermediate Combat":          "IntermediateCombatRecord",
		"Elementary Cognitive Carrier": "ElementaryCognitiveCarrier",
		"Intermediate Combat Record":   "IntermediateCombatRecord",
	} {
		got, ok := matchCreditItemID(text)
		if !ok || got != want {
			t.Fatalf("%q: got %q %v want %q", text, got, ok, want)
		}
	}
	for _, text := range []string{"Combat", "Elementary", "Advanced Combat", "Intermediate Cognitive", "Elementary Combat Collection"} {
		if got, ok := matchCreditItemID(text); ok {
			t.Fatalf("unrecognized item %q must not resolve to %q", text, got)
		}
	}
}
