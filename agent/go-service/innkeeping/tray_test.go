package innkeeping

import (
	"testing"
	"time"
)

func TestTraySelectionFollowsNextDinerInsteadOfCyclingAtOldTable(t *testing.T) {
	v := LiveView{Tray: map[string]int{"pilaf": 1, "soup": 1, "crackers": 1, "dumpling": 1}, TraySlots: []string{"pilaf", "soup", "crackers", "dumpling", ""}, SelectedSlot: 3}
	goals := []WorldFood{{Dish: "crackers", Box: [4]int{350, 200, 100, 40}}, {Dish: "pilaf", Box: [4]int{500, 245, 130, 60}}}
	if slot := nextDinerTraySlot(v, goals, [5]bool{}); slot != 0 {
		t.Fatalf("nearest pilaf request must select slot1 directly: %d", slot)
	}
	v.SelectedSlot = 0
	if slot := nextDinerTraySlot(v, goals, [5]bool{}); slot != -1 {
		t.Fatalf("selected next-diner dish must leave the old table: %d", slot)
	}
	v.SelectedSlot = 3
	if slot := nextDinerTraySlot(v, goals, [5]bool{true}); slot != -1 {
		t.Fatalf("cannot cycle through farther orders after trying the nearest dish: %d", slot)
	}
	if slot := nextDinerTraySlot(v, []WorldFood{{Dish: "unknown", Box: [4]int{500, 245, 130, 60}}}, [5]bool{}); slot != -1 {
		t.Fatalf("unknown request changed tray selection: %d", slot)
	}
}

func TestBatchPickupReservesCarriedCopiesAndCapacity(t *testing.T) {
	v := LiveView{Tray: map[string]int{"soup": 1}, Seats: []SeatView{{Dishes: []string{"soup", "cake"}}}}
	if neededPickup(v, "soup") || !neededPickup(v, "cake") {
		t.Fatal("carried soup was not reserved against demand")
	}
	v.Seats = append(v.Seats, SeatView{Dishes: []string{"soup"}})
	if !neededPickup(v, "soup") {
		t.Fatal("independent second soup order lost")
	}
	v.Tray["cake"] = 4
	if neededPickup(v, "soup") {
		t.Fatal("pickup exceeds five tray slots")
	}
}

func TestServingUsesSelectedSlotRatherThanAnotherHeldDish(t *testing.T) {
	v := LiveView{Phase: "active", Prompt: "serve", Tray: map[string]int{"soup": 2, "cake": 1},
		TraySlots: []string{"soup", "cake", "soup", "", ""}, SelectedSlot: 1}
	if _, ok := eligibleInput(v, pendingInput{Kind: "serve", Dish: "soup"}); ok {
		t.Fatal("selected cake could be recorded as served soup")
	}
	if _, ok := eligibleInput(v, pendingInput{Kind: "serve", Dish: "cake"}); !ok {
		t.Fatal("verified selected cake rejected because other dishes are carried")
	}
	v.SelectedSlot = 2
	if _, ok := eligibleInput(v, pendingInput{Kind: "serve", Dish: "soup"}); !ok {
		t.Fatal("two carried soup copies prevent serving the selected copy")
	}
	tried := [5]bool{true, true, true}
	if nextTraySlot(v, tried) != -1 {
		t.Fatal("incompatible dish selections can cycle indefinitely")
	}
}

func TestBatchCollectionEndsWhenDemandIsCovered(t *testing.T) {
	now := time.Now()
	s := liveSession{Jobs: []cookJob{{Dish: "soup", Phase: "ready"}, {Dish: "cake", Phase: "ready"}}}
	v := LiveView{Prompt: "put_back", Tray: map[string]int{"soup": 1}, Seats: []SeatView{{Dishes: []string{"soup", "cake"}}}}
	if !s.collectingBatch(v, now) {
		t.Fatal("ready cake should join the carried soup")
	}
	v.Tray["cake"] = 1
	if s.collectingBatch(v, now) {
		t.Fatal("covered demand kept the bot at the counter")
	}
	v.Tray["cake"] = 0
	s.Navigation.Departed = true
	if s.collectingBatch(v, now) {
		t.Fatal("delivery departure reversed to collect more food")
	}
}

func TestEnlargedDinerBubbleUsesBoundedNativeCompatibilityChecks(t *testing.T) {
	v := LiveView{Prompt: "serve", Tray: map[string]int{"fries": 1, "soup": 1, "dumpling": 1}, TraySlots: []string{"fries", "soup", "dumpling", "", ""}, SelectedSlot: 0}
	tried := [5]bool{true}
	if slot := nextDinerTraySlot(v, nil, tried); slot != 1 {
		t.Fatalf("unreadable enlarged bubble must allow one native soup compatibility check: %d", slot)
	}
	tried[1] = true
	v.SelectedSlot = 1
	if slot := nextDinerTraySlot(v, nil, tried); slot != 2 {
		t.Fatalf("remaining dumpling must be checked once: %d", slot)
	}
	tried[2] = true
	v.SelectedSlot = 2
	if slot := nextDinerTraySlot(v, nil, tried); slot != -1 {
		t.Fatalf("all incompatible slots must stop without cycling: %d", slot)
	}
}
