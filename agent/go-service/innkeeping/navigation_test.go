package innkeeping

import (
	"encoding/json"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/draw"
	"os"
	"testing"
	"time"
)

func navImage(t *testing.T, name string) image.Image {
	t.Helper()
	f, e := os.Open("testdata/" + name + ".png")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	im, _, e := image.Decode(f)
	if e != nil {
		t.Fatal(e)
	}
	return im
}
func TestWorldFoodGeometryOnActualFrames(t *testing.T) {
	lib, e := LoadIcons("../../../assets/data/Innkeeping/icons")
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, dish string
		counter    bool
	}{{"inn-world-counter", "hot_sour_soup", true},
		{"inn-world-choice-stack", "hot_sour_soup", true},
		{"inn-world-small-counter", "hot_sour_soup", true}, {"inn-world-diner", "hot_sour_soup", false}, {"inn-world-diner", "salted_cheese_crackers", false}} {
		t.Run(tc.name+tc.dish, func(t *testing.T) {
			found := WorldFoods(navImage(t, tc.name), lib, tc.counter)
			t.Logf("%+v", found)
			for _, f := range found {
				if f.Dish == tc.dish {
					return
				}
			}
			t.Fatal("actual destination marker was not recognized")
		})
	}
}

func TestCounterMarkerAtDistantScale(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	// A measured 31x36 marker becomes about 25x30 farther from the bar.
	// Keep the actual ring and dish pixels, including their downsampling loss.
	xdraw.CatmullRom.Scale(frame, image.Rect(580, 210, 608, 243), navImage(t, "inn-world-small-counter"), image.Rect(576, 212, 610, 252), draw.Src, nil)
	goals := WorldFoods(frame, lib, true)
	t.Logf("distant markers: %+v", goals)
	for _, goal := range goals {
		if goal.Dish == "hot_sour_soup" {
			return
		}
	}
	t.Fatalf("distant counter marker lost: %+v", goals)
}

func TestDistantDinersBelowHUDRemainVisible(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	goals := WorldFoods(navImage(t, "inn-distant-diners"), lib, false)
	t.Logf("distant diners: %+v", goals)
	for _, goal := range goals {
		if goal.Dish == "hot_sour_soup" {
			return
		}
	}
	t.Fatal("the requested soup below the HUD was not a diner destination")
}

func TestCompletedWorldFoodIsNotANavigationTarget(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	goals := WorldFoods(navImage(t, "inn-world-completed-bubble"), lib, false)
	crackers := false
	for _, goal := range goals {
		if goal.Box[0] < 500 || goal.Box[0] > 650 || goal.Box[3] < 60 {
			continue
		}
		if goal.Dish == "hot_sour_soup" {
			t.Fatal("green completed soup badge was ignored")
		}
		crackers = crackers || goal.Dish == "salted_cheese_crackers"
	}
	if !crackers {
		t.Fatalf("remaining crackers request lost: %+v", goals)
	}
}
func TestMenuSavedChoicesOnActualFrame(t *testing.T) {
	data, e := os.ReadFile("testdata/inn-menu-saved.json")
	if e != nil {
		t.Fatal(e)
	}
	var tokens []screenocr.Token
	if e = json.Unmarshal(data, &tokens); e != nil {
		t.Fatal(e)
	}
	d := planMenu(navImage(t, "inn-menu-saved"), tokens)
	if !d.Ready {
		t.Fatalf("saved four foods and three drinks should be ready: %+v", d)
	}
}

func TestMenuKeepsSelectedRecipesOutsideCurrentCategory(t *testing.T) {
	data, err := os.ReadFile("testdata/inn-menu-saved.json")
	if err != nil {
		t.Fatal(err)
	}
	var tokens []screenocr.Token
	if err = json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	visible := tokens[:0]
	for _, token := range tokens {
		if token.Box[1] >= 145 && token.Box[1] <= 505 && (foldName(token.Text) == foldName("Hot & Sour Soup") || foldName(token.Text) == foldName("Tuber Fries") || foldName(token.Text) == foldName("Fin Dumpling")) {
			continue
		}
		visible = append(visible, token)
	}
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	choice := planMenu(navImage(t, "inn-menu-saved"), visible, lib)
	if choice.Ready || choice.Box != [4]int{480, 90, 100, 40} {
		t.Fatalf("preserve selected recipes and return to All Dishes to verify their stock: %+v", choice)
	}
}
func TestNavigationDoesNotWalkTowardUnknownFood(t *testing.T) {
	n := navigationState{}
	now := time.Unix(1000, 0)
	cmd, reason := planMovement(&n, now, []WorldFood{{Dish: "other", Box: [4]int{700, 200, 100, 60}}}, map[string]bool{"soup": true}, false)
	if reason != "" || cmd.Kind != "camera" {
		t.Fatalf("unknown food caused movement: %+v %s", cmd, reason)
	}
}

func TestVisualNavigationHoldsDirectionInsteadOfSlowlyDragging(t *testing.T) {
	cmd := navigationGesture(movementCommand{Kind: "walk", X: 250, Y: 491, Milliseconds: 450})
	if !cmd.SharedJoystick || !cmd.Pipeline || cmd.Milliseconds != 20 || cmd.HoldMilliseconds != 450 {
		t.Fatalf("walk must hold the requested direction: %+v", cmd)
	}
	if again := navigationGesture(cmd); again != cmd {
		t.Fatal("already measured gesture timing changed")
	}
	camera := movementCommand{Kind: "camera", X: 910, Y: 350, Milliseconds: 250}
	rotated := navigationGesture(camera)
	if !rotated.Pipeline || rotated.Milliseconds != camera.Milliseconds || rotated.HoldMilliseconds != 0 || rotated.X != camera.X || rotated.Y != 230 || rotated.Origin != [2]int{1000, 230} {
		t.Fatal("camera must use the verified pipeline swipe without becoming a joystick hold")
	}
	if navigationGesture(rotated) != rotated {
		t.Fatal("camera origin translation must not compound on repeated preparation")
	}
}
func TestNavigationStopsAtStationaryTarget(t *testing.T) {
	n := navigationState{}
	now := time.Unix(1000, 0)
	g := []WorldFood{{Dish: "soup", Box: [4]int{700, 200, 100, 60}}}
	for i := 0; i < 3; i++ {
		_, r := planMovement(&n, now.Add(time.Duration(i)*time.Second), g, map[string]bool{"soup": true}, false)
		if r != "" {
			t.Fatal(r)
		}
	}
	if _, r := planMovement(&n, now.Add(3*time.Second), g, map[string]bool{"soup": true}, false); r == "" {
		t.Fatal("stationary target must stop movement")
	}
}
func TestNavigationSearchEndsAndResetsAfterDelivery(t *testing.T) {
	n := navigationState{}
	now := time.Unix(1000, 0)
	for i := 0; i < 16; i++ {
		c, r := planMovement(&n, now, nil, map[string]bool{"soup": true}, false)
		if r != "" || c.Kind != "camera" {
			t.Fatal("incomplete search")
		}
	}
	if _, r := planMovement(&n, now, nil, map[string]bool{"soup": true}, false); r == "" {
		t.Fatal("unbounded search")
	}
	if _, r := planMovement(&n, now, nil, map[string]bool{"soup": true}, true); r != "" {
		t.Fatal("return leg must reset search")
	}
}

func TestEmptyMenuWithOccludedTitle(t *testing.T) {
	data, e := os.ReadFile("testdata/inn-menu-empty.json")
	if e != nil {
		t.Fatal(e)
	}
	var tokens []screenocr.Token
	json.Unmarshal(data, &tokens)
	d := planMenu(navImage(t, "inn-menu-empty"), tokens)
	if d.Box[0] != 595 || d.Ready || d.Reason != "" {
		t.Fatalf("empty actual menu must select soup: %+v", d)
	}
}

func TestDrinkSelectionUsesStockAndMovedCounter(t *testing.T) {
	data, e := os.ReadFile("testdata/inn-menu-drinks.json")
	if e != nil {
		t.Fatal(e)
	}
	var tokens []screenocr.Token
	json.Unmarshal(data, &tokens)
	d := planMenu(navImage(t, "inn-menu-drinks"), tokens)
	if d.Box[0] != 595 || d.Ready || d.Reason != "" {
		t.Fatalf("actual drink menu must select stocked milk: %+v", d)
	}
}

func TestSlowVisibleProgressIsNotAStationaryTarget(t *testing.T) {
	n := navigationState{}
	now := time.Unix(1000, 0)
	for i := 0; i < 8; i++ {
		_, reason := planMovement(&n, now, []WorldFood{{Dish: "soup", Box: [4]int{700 + i*3, 200, 40, 40}}}, map[string]bool{"soup": true}, false)
		if reason != "" {
			t.Fatalf("cumulative marker movement was discarded: %s", reason)
		}
	}
}
func TestCollectionRequiresCompletedDemandedWork(t *testing.T) {
	now := time.Unix(1000, 0)
	seats := []SeatView{{Dishes: []string{"soup", "fries", "dumpling"}}}
	jobs := []cookJob{{Dish: "soup", ExpectedAt: now.Add(-time.Second)}, {Dish: "fries", ExpectedAt: now.Add(time.Second)}, {Dish: "cake", Phase: "ready"}, {Dish: "dumpling"}}
	ready := collectibleDishes(jobs, seats, now)
	if len(ready) != 1 || !ready["soup"] {
		t.Fatalf("uncooked or unwanted food became a navigation target: %+v", ready)
	}
	if len(collectibleDishes(jobs, nil, now)) != 0 {
		t.Fatal("unwanted completed food must not block cooking")
	}
	jobs[1].Phase = "ready"
	if !collectibleDishes(jobs, seats, now)["fries"] {
		t.Fatal("visually confirmed ready work should be collectible")
	}
}

func TestPickupRetiresReadyCopyBeforeFutureSameDish(t *testing.T) {
	now := time.Unix(1000, 0)
	jobs := []cookJob{{ID: "future", Dish: "soup", Kitchen: "1", ExpectedAt: now.Add(time.Minute)}, {ID: "ready", Dish: "soup", Kitchen: "2", Phase: "ready"}}
	got := retirePickedJob(jobs, "soup", now)
	if len(got) != 1 || got[0].ID != "future" {
		t.Fatalf("pickup removed future work: %+v", got)
	}
}

func TestReadyFoodReservesDemandWithoutOccupyingKitchenSlot(t *testing.T) {
	s := liveSession{Jobs: []cookJob{{Dish: "soup", Kitchen: "1", Phase: "ready"}, {Dish: "fries", Kitchen: "1", Phase: "cooking"}}, Kitchens: map[string]ChefKitchen{}}
	state := s.state(LiveView{})
	if state.Ready["soup"] != 1 || state.Kitchens[0].Queued["soup"] != 0 || state.Kitchens[0].Queued["fries"] != 1 {
		t.Fatalf("ready food and kitchen occupancy conflated: %+v", state)
	}
}

func TestAlternatingMarkersCannotResetNavigationLegDeadline(t *testing.T) {
	n := navigationState{}
	now := time.Unix(1000, 0)
	for i := 0; i < 12; i++ {
		var goals []WorldFood
		if i%2 == 0 {
			goals = []WorldFood{{Dish: "soup", Box: [4]int{500 + i*10, 200, 60, 40}}}
		}
		if _, r := planMovement(&n, now.Add(time.Duration(i*10)*time.Second), goals, map[string]bool{"soup": true}, false); r != "" {
			t.Fatalf("leg ended unexpectedly: %s", r)
		}
	}
	if _, r := planMovement(&n, now.Add(121*time.Second), []WorldFood{{Dish: "soup", Box: [4]int{620, 200, 60, 40}}}, map[string]bool{"soup": true}, false); r == "" {
		t.Fatal("reacquisition must not renew the entire navigation leg")
	}
}

func TestVisibleProgressRenewsNavigationWatchdog(t *testing.T) {
	n := navigationState{}
	now := time.Unix(1000, 0)
	for i := 0; i < 10; i++ {
		_, reason := planMovement(&n, now.Add(time.Duration(i*8)*time.Second), []WorldFood{{Dish: "manager", Box: [4]int{1100 - i*40, 180, 24, 24}}}, map[string]bool{"manager": true}, false)
		if reason != "" {
			t.Fatalf("approaching a visible manager timed out despite progress: %s", reason)
		}
	}
}

func TestCounterDepartureRequiresVisibleCounterAndDoesNotRepeat(t *testing.T) {
	n := navigationState{}
	now := time.Unix(1000, 0)
	if _, ok := counterDeparture(&n, now, nil); ok {
		t.Fatal("must not back away using an unseen counter")
	}
	cmd, ok := counterDeparture(&n, now, []WorldFood{{Dish: "cake", Counter: true, Box: [4]int{600, 250, 80, 80}}})
	if !ok || cmd.Kind != "leave_counter" || cmd.Y != 621 {
		t.Fatalf("visible counter must produce one departure: %+v", cmd)
	}
	if _, ok := counterDeparture(&n, now, []WorldFood{{Counter: true, Box: [4]int{600, 250, 80, 80}}}); ok {
		t.Fatal("departure must not become a backward movement loop")
	}
}

func TestMenuStopsBeforeStartingWithUnavailableIngredients(t *testing.T) {
	data, e := os.ReadFile("testdata/inn-menu-empty.json")
	if e != nil {
		t.Fatal(e)
	}
	var tokens []screenocr.Token
	json.Unmarshal(data, &tokens)
	for i := range tokens {
		if tokens[i].Box[0] > 450 && tokens[i].Box[0] < 600 && tokens[i].Box[1] > 220 && tokens[i].Box[1] < 260 {
			tokens[i].Text = "Preparable: 0"
			tokens[i].Score = .99
		}
	}
	d := planMenu(navImage(t, "inn-menu-empty"), tokens)
	if d.Ready || d.Box[2] > 0 || d.Reason == "" {
		t.Fatalf("unavailable ingredients must not start a session: %+v", d)
	}
}

func TestDisabledServeDoesNotTrapNavigationAtWrongTable(t *testing.T) {
	v := liveFixture(t, "inn-disabled-serve")
	if v.Prompt != "serve" || v.Tray["hot_sour_soup"] != 1 {
		t.Fatalf("actual incompatible diner frame was not read: %+v", v)
	}
	if actionableInteraction(navImage(t, "inn-disabled-serve"), v) {
		t.Fatal("disabled white Serve must allow continued movement")
	}
	green := liveFixture(t, "inn-table-two-approach")
	if !actionableInteraction(navImage(t, "inn-table-two-approach"), green) {
		t.Fatal("green compatible Serve should stop movement for delivery")
	}
}
func TestUnwantedPickupDoesNotTrapNavigation(t *testing.T) {
	v := LiveView{Prompt: "pickup", PromptDish: "cake", Tray: map[string]int{}, Seats: []SeatView{{Dishes: []string{"soup"}}}}
	if actionableInteraction(nil, v) {
		t.Fatal("unwanted food must not trap the collector")
	}
	v.Seats[0].Dishes = []string{"cake"}
	if !actionableInteraction(nil, v) {
		t.Fatal("matching ready food should yield to pickup")
	}
}

func TestInnOpenUsesCurrentInteractionListPosition(t *testing.T) {
	ys := []int{}
	for _, name := range []string{"inn-open-control-low", "inn-open-control-high"} {
		data, e := os.ReadFile("testdata/" + name + ".json")
		if e != nil {
			t.Fatal(e)
		}
		var tokens []screenocr.Token
		json.Unmarshal(data, &tokens)
		b, ok := innOpenControl(tokens)
		if !ok {
			t.Fatalf("actual world Open the Inn control not recovered: %s", name)
		}
		ys = append(ys, b[1])
	}
	if ys[0] == ys[1] {
		t.Fatal("the two live interaction list positions must remain distinct")
	}
	data, _ := os.ReadFile("testdata/inn-menu-saved.json")
	var tokens []screenocr.Token
	json.Unmarshal(data, &tokens)
	if _, ok := innOpenControl(tokens); ok {
		t.Fatal("world opener must not click the menu's session-start button")
	}
}

func TestActualDrinksBarChoosesTheClearFloorSide(t *testing.T) {
	v := liveFixture(t, "inn-drinks-bar")
	if v.Prompt != "bar" {
		t.Fatalf("actual bar interaction not read: %+v", v)
	}
	img := navImage(t, "inn-drinks-bar")
	if floorClearance(img, false) > .1 || floorClearance(img, true) < .5 {
		t.Fatal("cabinet and clear stone floor were not distinguished")
	}
	n := navigationState{}
	goals := []WorldFood{{Dish: "salted_cheese_crackers", Counter: true, Box: [4]int{452, 244, 64, 64}}}
	cmd, ok, reason := barDetour(&n, time.Now(), img, v, goals, map[string]bool{"salted_cheese_crackers": true})
	if !ok || reason != "" || cmd.X != 275 {
		t.Fatalf("bar approach must move through verified right-side floor: %+v %s", cmd, reason)
	}
	if _, ok, _ := barDetour(&n, time.Now(), img, v, goals, map[string]bool{"salted_cheese_crackers": true}); ok {
		t.Fatal("bar detour cannot repeat sideways indefinitely")
	}
}

func TestSlowOCRDoesNotTruncateBoundedCameraSearch(t *testing.T) {
	n := navigationState{}
	now := time.Unix(1000, 0)
	for i := 0; i < 16; i++ {
		c, r := planMovement(&n, now.Add(time.Duration(i*4)*time.Second), nil, map[string]bool{"manager": true}, false)
		if r != "" || c.Kind != "camera" {
			t.Fatalf("bounded search truncated by OCR latency at step %d: %s", i, r)
		}
	}
	if _, r := planMovement(&n, now.Add(64*time.Second), nil, map[string]bool{"manager": true}, false); r == "" {
		t.Fatal("camera search must still end")
	}
}

func TestTargetFoundAfterSlowSearchStartsMovement(t *testing.T) {
	now := time.Unix(1000, 0)
	n := navigationState{Goal: "diner", Since: now, Scanned: 900}
	c, r := planMovement(&n, now.Add(50*time.Second), []WorldFood{{Dish: "manager", Box: [4]int{600, 180, 24, 24}}}, map[string]bool{"manager": true}, false)
	if r != "" || c.Kind != "walk" {
		t.Fatalf("fresh target rejected after search: %+v %s", c, r)
	}
}

func TestLastDishDepartureUsesFreshVerifiedPickupOnly(t *testing.T) {
	n := navigationState{PickupZone: true}
	c, ok := counterDeparture(&n, time.Unix(1000, 0), nil)
	if !ok || c.Kind != "leave_counter" || c.X != 195 || c.Y != 621 {
		t.Fatalf("last dish must leave confirmed pickup zone: %+v", c)
	}
	if _, ok = counterDeparture(&n, time.Unix(1001, 0), nil); ok {
		t.Fatal("departure repeated")
	}
	n = navigationState{PickupZone: true, Scanned: 90}
	if _, ok = counterDeparture(&n, time.Unix(1000, 0), nil); ok {
		t.Fatal("cannot infer counter direction after camera rotation")
	}
}

func TestHeldFoodLeavesCounterWithAnotherPickupPrompt(t *testing.T) {
	v := liveFixture(t, "inn-held-pickup-zone")
	if v.Tray["hot_sour_soup"] != 1 || v.SelectedSlot != 0 || v.Prompt != "pickup" || !atPickupCounter(v) {
		t.Fatalf("fresh pickup zone was lost: %+v", v)
	}
	v.Tray = map[string]int{}
	if atPickupCounter(v) {
		t.Fatal("empty tray must collect before departing")
	}
}

func TestTouchingCounterRingsRemainCollectible(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	found := WorldFoods(navImage(t, "inn-counter-overlap"), lib, true)
	t.Logf("merged counter destinations: %+v", found)
	for _, f := range found {
		if f.Dish == "hot_sour_soup" {
			return
		}
	}
	t.Fatal("the exposed soup ring was lost when ready-food markers touched")
}
func TestDinerCapsuleEndsAreNotSplitIntoCounterGoals(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	img := navImage(t, "inn-world-diner")
	for _, box := range [][4]int{{444, 234, 164, 68}, {832, 214, 120, 52}} {
		if found := splitCounterRings(img, lib, box); len(found) > 0 {
			t.Fatalf("diner capsule became counter rings: %+v", found)
		}
	}
}

func TestCounterFurnitureCannotBecomeAnUncookedDumpling(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	found := WorldFoods(navImage(t, "inn-counter-negative"), lib, true)
	t.Logf("counter negative: %+v", found)
	for _, f := range found {
		if f.Dish == "fin_dumpling" {
			t.Fatalf("furniture fragment became food: %+v", f)
		}
	}
}

func TestNeighboringStackFrameStillYieldsSoup(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	found := WorldFoods(navImage(t, "inn-counter-overlap-neighbor"), lib, true)
	t.Logf("neighboring stack: %+v", found)
	for _, f := range found {
		if f.Dish == "hot_sour_soup" {
			return
		}
	}
	t.Fatal("slight camera movement lost the exposed front soup marker")
}
func TestSmallDinerCapsulesAreNotCounterDestinations(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range WorldFoods(navImage(t, "inn-world-choice-stack"), lib, true) {
		if f.Box[0] < 1000 {
			t.Fatalf("diner capsule interior became a counter goal: %+v", f)
		}
	}
}

func TestReadyObservationAndPickupUseTheSameCompletedCopy(t *testing.T) {
	now := time.Unix(1000, 0)
	jobs := []cookJob{{ID: "slow", Dish: "soup", Kitchen: "1", ExpectedAt: now.Add(10 * time.Second)}, {ID: "fast", Dish: "soup", Kitchen: "2", ExpectedAt: now.Add(-time.Second)}}
	i := trackedCopyIndex(jobs, "soup", now)
	if i != 1 {
		t.Fatal("ready prompt was assigned to future slow-kitchen work")
	}
	jobs[i].Phase = "ready"
	state := (&liveSession{Jobs: jobs, Kitchens: map[string]ChefKitchen{}}).state(LiveView{})
	if state.Ready["soup"] != 1 || state.Kitchens[0].Queued["soup"] != 1 || state.Kitchens[1].Queued["soup"] != 0 {
		t.Fatalf("wrong kitchen slot freed: %+v", state)
	}
	got := retirePickedJob(jobs, "soup", now)
	if len(got) != 1 || got[0].ID != "slow" {
		t.Fatalf("ready observation and pickup disagreed: %+v", got)
	}
}
func TestDepartureDoesNotInferBackwardDirectionAfterCameraSearch(t *testing.T) {
	n := navigationState{Goal: "diner", Scanned: 90}
	if _, ok := counterDeparture(&n, time.Unix(1000, 0), []WorldFood{{Counter: true, Box: [4]int{600, 250, 80, 80}}}); ok {
		t.Fatal("rotated counter proximity cannot prove the departure direction")
	}
}
func TestBarDetourPreservesAndHonorsLegDeadline(t *testing.T) {
	now := time.Unix(1000, 0)
	img := navImage(t, "inn-drinks-bar")
	v := LiveView{Prompt: "bar"}
	goals := []WorldFood{{Dish: "soup", Counter: true, Box: [4]int{450, 220, 50, 50}}}
	n := navigationState{Goal: "counter", LegStarted: now.Add(-time.Minute)}
	if _, ok, r := barDetour(&n, now, img, v, goals, map[string]bool{"soup": true}); r != "" || !ok {
		t.Fatalf("measured clear-floor detour rejected: %s", r)
	}
	if !n.LegStarted.Equal(now.Add(-time.Minute)) {
		t.Fatal("detour renewed the whole movement budget")
	}
	n = navigationState{Goal: "counter", LegStarted: now.Add(-121 * time.Second)}
	if _, ok, r := barDetour(&n, now, img, v, goals, map[string]bool{"soup": true}); ok || r == "" {
		t.Fatal("expired movement leg issued a detour")
	}
}

func TestRecordedRoutesDoNotSwitchJoystickDriver(t *testing.T) {
	original := movementCommand{Kind: "walk", X: 195, Y: 471, Milliseconds: 20, HoldMilliseconds: 450, Pipeline: true}
	if got := navigationGesture(original); got != original || got.SharedJoystick {
		t.Fatalf("recorded gesture changed: %+v", got)
	}
}

func TestCounterPickupMarkerNearLeftEdge(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	source := navImage(t, "inn-world-counter")
	goals := WorldFoods(source, lib, true)
	if len(goals) == 0 {
		t.Fatal("fixture has no ready food")
	}
	target := goals[0]
	shift := target.Box[0] + target.Box[2]/2 - 120
	frame := image.NewRGBA(source.Bounds())
	draw.Draw(frame, frame.Bounds(), source, image.Pt(shift, 0), draw.Src)
	for _, g := range WorldFoods(frame, lib, true) {
		if g.Dish == target.Dish && g.Box[0] < 160 {
			return
		}
	}
	t.Fatal("visible first counter pickup was excluded by the search region")
}

func TestNearCounterRingBelowOldSearchBoundary(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range WorldFoods(navImage(t, "inn-tray-wrapped"), lib, true) {
		if g.Dish == "salted_cheese_crackers" && g.Box[1]+g.Box[3] > 480 && g.Box[3] > 120 {
			return
		}
	}
	t.Fatal("large visible near-counter ring was clipped before classification")
}

func TestCounterSoupRingDoesNotMergeIntoWoodenCounter(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		x    int
	}{{"inn-counter-connected-floor", 320}, {"inn-counter-connected-floor-left", 240}} {
		t.Run(tc.name, func(t *testing.T) {
			crop := navImage(t, tc.name)
			frame := image.NewRGBA(image.Rect(0, 0, 1280, 720))
			draw.Draw(frame, image.Rect(tc.x, 300, tc.x+320, 460), crop, crop.Bounds().Min, draw.Src)
			for _, food := range WorldFoods(frame, lib, true) {
				if food.Dish == "hot_sour_soup" && food.Box[0] >= tc.x && food.Box[0] < tc.x+120 && food.Box[1] >= 300 && food.Box[3] >= 60 {
					return
				}
			}
			t.Fatal("visible soup was lost where its ring touches bright wooden furniture")
		})
	}
}
