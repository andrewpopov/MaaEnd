package innkeeping

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

func TestCenteredSingleOrderOnActualFrame(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	img := navImage(t, "inn-single-congee")
	goals := WorldFoods(img, lib, false)
	if len(goals) != 1 || goals[0].Dish != "tonic_congee_greens" {
		t.Fatalf("centered one-item world request missing: %+v", goals)
	}
	tokens := []screenocr.Token{{Text: "Current Income", Score: 1}, {Text: "Patrons Seated", Score: 1}, {Text: "Dishes Sold", Score: 1}}
	v := ReadLive(img, tokens, lib, nil)
	if len(v.Seats) != 6 || len(v.Seats[2].Dishes) != 1 || v.Seats[2].Dishes[0] != "tonic_congee_greens" {
		t.Fatalf("centered one-item HUD request missing or duplicated: %+v", v.Seats)
	}
}
