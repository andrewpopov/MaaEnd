package innkeeping

import "testing"

func TestDistantCounterPair(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	img := navImage(t, "inn-distant-counter-pair")
	goals := WorldFoods(img, lib, true)
	t.Logf("goals: %+v", goals)
	for _, wanted := range []string{"meaty_pilaf", "salted_cheese_crackers"} {
		found := false
		for _, goal := range goals {
			found = found || goal.Dish == wanted
		}
		if !found {
			t.Errorf("missed visible ready %s", wanted)
		}
	}
}
