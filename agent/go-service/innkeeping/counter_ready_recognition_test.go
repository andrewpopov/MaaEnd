package innkeeping

import "testing"

func TestReadyFoodOffsetRequiresCounterIdentityAndDirection(t *testing.T) {
	goals := []WorldFood{{Dish: "fin", Counter: false, Box: [4]int{900, 250, 80, 80}}, {Dish: "soup", Counter: true, Box: [4]int{900, 250, 80, 80}}, {Dish: "fin", Counter: true, Box: [4]int{420, 250, 80, 80}}, {Dish: "fin", Counter: true, Box: [4]int{600, 250, 80, 80}}}
	if _, ok := readyFoodOffset(goals, "fin", "right"); ok {
		t.Fatal("diner marker, other food or central control authorized right movement")
	}
	if g, ok := readyFoodOffset(goals, "fin", "left"); !ok || g.Box[0] != 420 {
		t.Fatal("visible desired left counter slot rejected")
	}
	if _, ok := readyFoodOffset(goals, "fin", "up"); ok {
		t.Fatal("unsupported direction accepted")
	}
	if _, ok := readyFoodOffset(goals, "unknown", "left"); ok {
		t.Fatal("unknown dish authorized movement")
	}
}

func TestReadyFoodCenteredApproachStillRequiresKnownCounterFood(t *testing.T) {
	goals := []WorldFood{{Dish: "fin", Counter: false, Box: [4]int{600, 250, 80, 80}}, {Dish: "soup", Counter: true, Box: [4]int{600, 250, 80, 80}}, {Dish: "fin", Counter: true, Box: [4]int{900, 250, 80, 80}}}
	if _, ok := readyFoodOffset(goals, "fin", "center"); ok {
		t.Fatal("unrelated or noncentral marker authorized depth correction")
	}
	goals = append(goals, WorldFood{Dish: "fin", Counter: true, Box: [4]int{560, 250, 80, 80}})
	if g, ok := readyFoodOffset(goals, "fin", "center"); !ok || g.Box[0] != 560 {
		t.Fatal("known central counter marker rejected")
	}
}
