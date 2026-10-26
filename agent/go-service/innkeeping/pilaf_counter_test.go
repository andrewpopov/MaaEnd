package innkeeping

import "testing"

func TestPilafCounterMarker(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	img := navImage(t, "inn-pilaf-counter")
	goals := WorldFoods(img, lib, true)
	t.Logf("goals: %+v", goals)
	for _, goal := range goals {
		if goal.Dish == "meaty_pilaf" {
			return
		}
	}
	t.Fatal("visible ready Pilaf marker was not recognized")
}
