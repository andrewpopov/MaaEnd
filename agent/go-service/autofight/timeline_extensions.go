package autofight

// extendedTimelineAction adds fork action types after the upstream parser has
// handled its native types. Ordering and action metadata stay with that parser.
func extendedTimelineAction(kind string) (string, bool) {
	switch kind {
	case "comboSkill", "combo":
		return "combo", true
	default:
		return "", false
	}
}
