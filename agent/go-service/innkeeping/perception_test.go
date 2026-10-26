package innkeeping

import (
	"encoding/json"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"
)

func liveFixture(t *testing.T, name string) LiveView {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var tokens []screenocr.Token
	if err = json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	dishes, err := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if err != nil {
		t.Fatal(err)
	}
	return ReadLive(im, tokens, lib, dishes)
}
func TestRealLiveViews(t *testing.T) {
	for _, name := range []string{"inn-pending-queue", "inn-final-k2", "inn-live-failure", "inn-live-k2", "inn-live-k1", "inn-preparation", "inn-ready-counter", "inn-live-kitchen-two", "inn-soup-queued", "inn-dumpling-queued", "inn-fin-held", "inn-first-serve", "inn-table-two-approach", "inn-trial-ended"} {
		t.Run(name, func(t *testing.T) {
			v := liveFixture(t, name)
			b, _ := json.Marshal(v)
			t.Log(string(b))
			if len(v.Unknown) > 0 {
				t.Fatalf("calibrated view rejected: %v", v.Unknown)
			}
			switch name {
			case "inn-pending-queue":
				if !v.QueueVerified || v.Queue["hot_sour_soup"] != 1 || v.Queue["tuber_fries"] != 1 {
					t.Fatalf("pending and active queue misread: %+v", v.Queue)
				}
			case "inn-final-k2":
				if v.Kitchen != "2" || len(v.Recipes) != 4 {
					t.Fatal("kitchen two lost")
				}
				for _, r := range v.Recipes {
					if !r.Available || (r.Dish == "hot_sour_soup" && r.Seconds != 0) {
						t.Fatalf("implausible header must request focused OCR: %+v", r)
					}
				}
			case "inn-live-failure":
				if v.Queue["hot_sour_soup"] != 1 || !v.QueueVerified {
					t.Fatal("queue glow rejected")
				}
				if len(v.Seats[0].Dishes) > 0 || len(v.Seats[1].Dishes) > 0 || len(v.Seats[3].Dishes) > 0 || len(v.Seats[4].Dishes) > 0 {
					t.Fatal("empty seat treated as an order")
				}
			case "inn-live-k2":
				if len(v.Seats[2].Dishes) > 0 {
					t.Fatal("ordering seat treated as food")
				}
				for i, seat := range v.Seats {
					if i != 2 && len(seat.Dishes) > 0 {
						t.Fatal("empty seat became food")
					}
				}
				found := false
				for _, r := range v.Recipes {
					if r.Dish == "hot_sour_soup" && r.Available && r.Seconds == 34 {
						found = true
					}
				}
				if !found {
					t.Fatal("eligible faster kitchen not recognized")
				}
			case "inn-preparation":
				if v.Phase != "preparation" || v.RemainingSeconds != 0 || len(v.Recipes) > 0 {
					t.Fatal("preparation must never start the live session")
				}
			case "inn-ready-counter":
				if v.Phase != "active" || len(v.Seats[2].Dishes) != 2 || v.Seats[5].Phase != "gourmand" {
					t.Fatal("six-seat/two-food request not read")
				}
			case "inn-live-kitchen-two":
				if v.Phase != "kitchen" || v.Kitchen != "2" || len(v.Queue) != 0 || len(v.Recipes) != 4 {
					t.Fatal("second kitchen not read")
				}
			case "inn-soup-queued":
				if v.Queue["hot_sour_soup"] != 1 || !v.QueueVerified {
					t.Fatal("soup cook not verified")
				}
			case "inn-dumpling-queued":
				if v.Queue["fin_dumpling"] != 1 || v.Kitchen != "1" {
					t.Fatal("dumpling cook not verified")
				}
			case "inn-fin-held":
				if v.Tray["fin_dumpling"] != 1 || v.Prompt != "put_back" {
					t.Fatal("held food not read")
				}
			case "inn-first-serve":
				if len(v.Tray) != 0 || v.Seats[1].Phase != "dining" {
					t.Fatal("serve not verified")
				}
			case "inn-table-two-approach":
				if v.Prompt != "serve" || v.Tray["fin_dumpling"] != 1 {
					t.Fatal("serve control not read")
				}
			case "inn-trial-ended":
				if v.Phase != "ended" {
					t.Fatal("results screen must stop assistance")
				}
			}
		})
	}
}

func TestFocusedTimeResolvesHourglassExtraDigit(t *testing.T) {
	f, _ := os.Open("testdata/inn-final-k2.png")
	defer f.Close()
	im, _, _ := image.Decode(f)
	data, _ := os.ReadFile("testdata/inn-final-k2.json")
	var tokens []screenocr.Token
	json.Unmarshal(data, &tokens)
	focused, _ := os.ReadFile("testdata/inn-final-k2-time.json")
	var extra []screenocr.Token
	json.Unmarshal(focused, &extra)
	tokens = append(tokens, extra...)
	lib, _ := LoadIcons("../../../assets/data/Innkeeping/icons")
	dishes, _ := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	v := ReadLive(im, tokens, lib, dishes)
	for _, r := range v.Recipes {
		if r.Dish == "hot_sour_soup" {
			if r.Seconds != 34 {
				t.Fatalf("focused badge must read 34: %+v", r)
			}
			return
		}
	}
	t.Fatal("soup missing")
}

func TestActualQueueTimerRestartConfirmsCook(t *testing.T) {
	before := liveFixture(t, "inn-race-before")
	after := liveFixture(t, "inn-race-after")
	if before.ActiveSeconds != 2 || after.ActiveSeconds != 36 {
		t.Fatalf("actual timer transition lost: %.0f -> %.0f", before.ActiveSeconds, after.ActiveSeconds)
	}
	p := pendingInput{Kind: "queue", Kitchen: before.Kitchen, Dish: "hot_sour_soup", Before: before.Queue["hot_sour_soup"], BeforeSeconds: before.ActiveSeconds}
	if !queuedInputConfirmed(p, after) {
		t.Fatal("actual same-count cook restart must be confirmed")
	}
}

func TestRealServePromptEnabledColor(t *testing.T) {
	f, _ := os.Open("testdata/inn-table-two-approach.png")
	defer f.Close()
	im, _, _ := image.Decode(f)
	if !greenServeControl(im, [4]int{808, 411, 61, 27}) {
		t.Fatal("real enabled Serve prompt must be actionable")
	}
	if greenServeControl(im, [4]int{914, 640, 86, 23}) {
		t.Fatal("white Add Order text is not an enabled Serve prompt")
	}
}

func TestTraySelectionAndWrappedPromptOnActualFrame(t *testing.T) {
	v := liveFixture(t, "inn-tray-wrapped")
	if v.SelectedSlot != 0 || v.TraySlots[0] != "tuber_fries" {
		t.Fatalf("white selected outline must override yellow pickup arrow: %+v", v)
	}
	if v.Prompt != "pickup" || v.PromptDish != "salted_cheese_crackers" {
		t.Fatalf("complete wrapped interaction label not recognized: %+v", v)
	}
	v = liveFixture(t, "inn-two-second-selected")
	if v.SelectedSlot != 1 || v.TraySlots[1] != "hot_sour_soup" {
		t.Fatalf("actual selected second dish lost: %+v", v)
	}
	p := pendingInput{Kind: "select_tray", Dish: "fin_dumpling", Slot: 0}
	if _, ok := eligibleInput(v, p); !ok {
		t.Fatal("occupied slot correction rejected")
	}
	p.Slot = 1
	if _, ok := eligibleInput(v, p); ok {
		t.Fatal("empty slot accepted")
	}
	if selectedTraySlot(navImage(t, "inn-tray-first")) != 0 {
		t.Fatal("first-slot outline not recognized")
	}
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range WorldFoods(navImage(t, "inn-tray-wrapped"), lib, true) {
		if f.Box[0] >= 700 && f.Box[0] < 810 && f.Box[1] >= 390 {
			t.Fatalf("interaction icon treated as a world destination: %+v", f)
		}
	}
}

func TestVisibleActiveRecipeIsNotAnotherQueueRequest(t *testing.T) {
	v := liveFixture(t, "inn-soup-queued")
	for _, r := range v.Recipes {
		if r.Dish == "hot_sour_soup" {
			if !r.Available {
				t.Fatal("infinite ingredient stock was lost")
			}
			if recipeQueueable(v, r) {
				t.Fatal("already-active soup was offered as another queue request")
			}
			return
		}
	}
	t.Fatal("real soup card was not recognized")
}

func TestWrappedPromptDeduplicatesOverlappingOCRReads(t *testing.T) {
	tokens := []screenocr.Token{
		{Text: "Salted Cheese", Score: .96, Box: [4]int{813, 408, 100, 15}},
		{Text: "Crackers", Score: .99, Box: [4]int{810, 422, 67, 22}},
		{Text: "Salted Cheese", Score: .99, Box: [4]int{811, 408, 101, 16}},
		{Text: "Crackers", Score: .99, Box: [4]int{811, 422, 66, 22}},
	}
	v := LiveView{}
	readWorldPrompt(&v, tokens, map[string]string{"saltedcheesecrackers": "salted_cheese_crackers"})
	if v.Prompt != "pickup" || v.PromptDish != "salted_cheese_crackers" {
		t.Fatalf("duplicate OCR hid complete wrapped pickup: %+v", v)
	}
	if v.PromptBox[0] != 810 || v.PromptBox[1] != 408 {
		t.Fatalf("lost actual control bounds: %+v", v.PromptBox)
	}
}

func TestServedFoodBadgePrecedesDiningLabel(t *testing.T) {
	v := liveFixture(t, "inn-food-served-card")
	seat := v.Seats[1]
	if seat.Phase != "served" || len(seat.Dishes) != 0 || !includes(seat.Served, "hot_sour_soup") {
		t.Fatalf("explicit served badge was lost before Dining text: %+v", seat)
	}
}
