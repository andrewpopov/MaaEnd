package autofight

import "slices"

// A shared combo button is safe for an ordered step only when exactly the
// intended operator owns the current prompt. Ambiguous prompts wait for evidence.
func comboDispatchAllowed(sa *ScreenAnalyzer, slot int) bool {
	if sa == nil || !sa.GetCharacterComboActive() {
		return false
	}
	if slot == 0 {
		return true
	}
	owners := sa.currentComboSlots()
	return len(owners) == 1 && slices.Contains(owners, slot)
}
