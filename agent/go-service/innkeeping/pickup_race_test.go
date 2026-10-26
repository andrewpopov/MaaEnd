package innkeeping

import (
	"image"
	"os"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

func TestActualPickupRaceTray(t *testing.T) {
	f, err := os.Open("testdata/inn-pickup-race-tray.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	// Masked fixture retains just the tray; the phase labels are unchanged on
	// the original active-session frame, but are outside the retained region.
	tokens := []screenocr.Token{{Text: "Current Income", Score: 1}, {Text: "Patrons Seated", Score: 1}, {Text: "Dishes Sold", Score: 1}}
	v := ReadLive(img, tokens, lib, nil)
	v.Seats = []SeatView{{Seat: "3", Dishes: []string{"hot_sour_soup"}}}
	p := pendingInput{Dish: "salted_cheese_crackers", TrayBefore: map[string]int{}}
	if got := confirmedPickup(p, v, []cookJob{{Dish: "hot_sour_soup"}}); got != "hot_sour_soup" {
		t.Fatalf("the actual captured tray must verify soup: %q, view=%+v", got, v)
	}
}

func TestPickupCompletionRaceUsesVerifiedTrayGain(t *testing.T) {
	p := pendingInput{Kind: "pickup", Dish: "salted_cheese_crackers", TrayBefore: map[string]int{}}
	v := LiveView{Phase: "active", Tray: map[string]int{"hot_sour_soup": 1}, Seats: []SeatView{{Seat: "3", Dishes: []string{"hot_sour_soup"}}}}
	jobs := []cookJob{{Dish: "salted_cheese_crackers"}, {Dish: "hot_sour_soup"}}
	if actual := confirmedPickup(p, v, jobs); actual != "hot_sour_soup" {
		t.Fatalf("new soup finished during the cracker click; actual tray gain must retire the soup job: %q", actual)
	}
	// The same rule verifies the intended pickup while preserving carried food.
	p.TrayBefore = map[string]int{"hot_sour_soup": 1}
	v.Tray["salted_cheese_crackers"] = 1
	v.Seats = append(v.Seats, SeatView{Seat: "1", Dishes: []string{"salted_cheese_crackers"}})
	if actual := confirmedPickup(p, v, jobs); actual != "salted_cheese_crackers" {
		t.Fatalf("expected exact one-copy gain with soup preserved: %q", actual)
	}
}

func TestPickupRaceCannotHideUnexplainedInventoryChange(t *testing.T) {
	for _, change := range []string{"untracked", "unneeded", "two gained", "other lost", "unknown", "kitchen", "no before"} {
		t.Run(change, func(t *testing.T) {
			p := pendingInput{Dish: "cracker", TrayBefore: map[string]int{"pilaf": 1}}
			v := LiveView{Phase: "active", Tray: map[string]int{"pilaf": 1, "soup": 1}, Seats: []SeatView{{Dishes: []string{"soup"}}}}
			jobs := []cookJob{{Dish: "soup"}}
			switch change {
			case "untracked":
				jobs = nil
			case "unneeded":
				v.Seats = nil
			case "two gained":
				v.Tray["soup"] = 2
			case "other lost":
				v.Tray = map[string]int{"soup": 2}
			case "unknown":
				v.Unknown = []string{"tray slot 2"}
			case "kitchen":
				v.Phase = "kitchen"
			case "no before":
				p.TrayBefore = nil
			}
			if got := confirmedPickup(p, v, jobs); got != "" {
				t.Fatalf("must stop for unexplained pickup: %q", got)
			}
		})
	}
}

func TestPickupCannotSnapshotAnUnknownBeforeTray(t *testing.T) {
	v := LiveView{Phase: "active", Prompt: "pickup", PromptDish: "cracker", Tray: map[string]int{}, Seats: []SeatView{{Dishes: []string{"cracker"}}}}
	p := pendingInput{Dish: "cracker", Kind: "pickup"}
	if _, ok := eligibleInput(v, p); !ok {
		t.Fatal("known empty tray is eligible")
	}
	v.Unknown = []string{"tray slot 1"}
	if _, ok := eligibleInput(v, p); ok {
		t.Fatal("an unrecognized carried item must not be mistaken for a later pickup gain")
	}
}
