package innkeeping

import "testing"

func TestShadedReadyCounterFoodRemainsVisible(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	frame := navImage(t, "inn-counter-shaded-ready")
	seen := map[string]bool{}
	for _, food := range WorldFoods(frame, lib, true) {
		seen[food.Dish] = true
		t.Logf("%+v", food)
	}
	for _, dish := range []string{"salted_cheese_crackers", "shroom_dumpling"} {
		if !seen[dish] {
			t.Errorf("visible ready dish %s was lost", dish)
		}
	}
}
