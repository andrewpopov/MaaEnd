package innkeeping

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"testing"
	"time"
)

func gourmandSessionFixture(t *testing.T) *liveSession {
	t.Helper()
	catalog, e := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if e != nil {
		t.Fatal(e)
	}
	return &liveSession{Started: true, Catalog: catalog, Tray: map[string]int{}, Kitchens: map[string]ChefKitchen{"2": {ID: "2", Verified: true, Capacity: 3, Available: map[string]bool{"hot_sour_soup": true, "salted_cheese_crackers": true}, CookSeconds: map[string]float64{"hot_sour_soup": 34, "salted_cheese_crackers": 15}}}}
}
func gourmandSessionView() LiveView {
	return LiveView{Phase: "active", GourmandBubble: [4]int{450, 200, 250, 100}, GourmandTextComplete: true, RemainingSeconds: 180, Tray: map[string]int{}, Seats: []SeatView{{Seat: "1", Phase: "gourmand"}}, GourmandRequests: []GourmandRequestView{{Text: "I want a sour dish without flour", Requirements: ParseGourmandRequest("I want a sour dish without flour")}, {Text: "I want a salty dessert", Requirements: ParseGourmandRequest("I want a salty dessert")}}}
}
func TestGourmandSessionFlowsThroughCookingPickupAndServing(t *testing.T) {
	s := gourmandSessionFixture(t)
	v := s.observeGourmand(gourmandSessionView(), nil, func([4]int, string) bool { return false })
	if v.GourmandSeat != "1" || len(v.Seats[0].Dishes) != 2 {
		t.Fatalf("sole gourmand popup not bound: %+v", v)
	}
	state := s.stateAt(v, time.Now())
	if len(state.Demands) != 2 || !state.Demands[0].Gourmand {
		t.Fatalf("gourmand demands not wired: %+v", state.Demands)
	}
	d := PlanReactive(state)
	if d.Kind != "queue" || d.Dish != "hot_sour_soup" {
		t.Fatalf("verified serving not queued: %+v", d)
	}
	s.Jobs = []cookJob{{Dish: "hot_sour_soup", Kitchen: "2", Phase: "ready"}}
	v.Tray = map[string]int{}
	if !neededPickup(s.gourmandView(v), "hot_sour_soup") || !readyForTray(s.Jobs, s.gourmandView(v), time.Now())["hot_sour_soup"] {
		t.Fatal("gourmand cooking cannot reach pickup")
	}
	s.confirmGourmandServing(pendingInput{Gourmand: true, Seat: "1", GourmandSlot: 0, GourmandVisit: 0, Dish: "hot_sour_soup"})
	v = s.observeGourmand(gourmandSessionView(), nil, func([4]int, string) bool { return false })
	if len(v.Seats[0].Dishes) != 1 || v.Seats[0].Dishes[0] != "salted_cheese_crackers" {
		t.Fatal("repeated popup resurrected served soup")
	}
	if len(s.stateAt(v, time.Now()).Demands) != 1 {
		t.Fatal("served plate still authorizes cooking")
	}
	s.confirmGourmandServing(pendingInput{Gourmand: true, Seat: "1", GourmandSlot: 1, GourmandVisit: 0, Dish: "salted_cheese_crackers"})
	if len(s.stateAt(v, time.Now()).Demands) != 0 {
		t.Fatal("two served plates did not retire both requests")
	}
}
func TestTwoGourmandsRequireVerifiedPhysicalNumber(t *testing.T) {
	s := gourmandSessionFixture(t)
	v := gourmandSessionView()
	v.Seats = append(v.Seats, SeatView{Seat: "5", Phase: "gourmand"})
	if got := s.observeGourmand(v, nil, func([4]int, string) bool { return false }); got.GourmandSeat != "" || len(s.Gourmands) != 0 {
		t.Fatal("ambiguous popup assigned to a guessed table")
	}
	tokens := []screenocr.Token{{Text: "5", Score: .6, Box: [4]int{460, 312, 45, 59}}}
	v = s.observeGourmand(v, tokens, func(_ [4]int, seat string) bool { return seat == "5" })
	if v.GourmandSeat != "5" || len(v.Seats[0].Dishes) != 0 || len(v.Seats[1].Dishes) != 2 {
		t.Fatalf("verified board did not select table5: %+v", v)
	}
	tokens = append(tokens, screenocr.Token{Text: "1", Score: .99, Box: [4]int{700, 300, 30, 40}})
	if gourmandSeat(v, tokens, func([4]int, string) bool { return true }) != "" {
		t.Fatal("two verified boards accepted as one seat")
	}
}
func TestGourmandVisitAndOrdinaryServingDoNotConsumeWrongOrder(t *testing.T) {
	s := gourmandSessionFixture(t)
	v := s.observeGourmand(gourmandSessionView(), nil, func([4]int, string) bool { return false })
	s.confirmGourmandServing(pendingInput{Seat: "1", Dish: "hot_sour_soup"})
	if s.Gourmands["1"].Served[0] {
		t.Fatal("ordinary soup served elsewhere consumed gourmand plate")
	}
	s.Visit[0]++
	s.confirmGourmandServing(pendingInput{Gourmand: true, Seat: "1", GourmandSlot: 0, GourmandVisit: 0, Dish: "hot_sour_soup"})
	if s.Gourmands["1"].Served[0] {
		t.Fatal("old acknowledgement consumed a new guest's plate")
	}
	if len(s.gourmandView(v).Seats[0].Dishes) != 0 {
		t.Fatal("old visit projected food onto new guest")
	}
}

func TestChangedGourmandRequestDoesNotRecreateServedFood(t *testing.T) {
	s := gourmandSessionFixture(t)
	s.observeGourmand(gourmandSessionView(), nil, func([4]int, string) bool { return false })
	v := gourmandSessionView()
	v.GourmandRequests[0].Text = "I want a sour soup"
	s.observeGourmand(v, nil, func([4]int, string) bool { return false })
	v = s.observeGourmand(v, nil, func([4]int, string) bool { return false })
	if len(v.Seats[0].Dishes) != 0 || !s.Gourmands["1"].Blocked {
		t.Fatal("changed request recreated a partially completed visit")
	}
}
func TestLiveSingleDessertRequestBindsAndQueuesOnePlate(t *testing.T) {
	tokens := []screenocr.Token{{Text: "0/1", Score: .998, Box: [4]int{424, 276, 28, 19}}, {Text: "Dessert, please.", Score: .994, Box: [4]int{416, 303, 156, 29}}}
	requests := readGourmandRequestsCount(tokens, 1)
	if len(requests) != 1 || !includes(requests[0].Requirements.IncludeTypes, "dessert") {
		t.Fatalf("actual single serving lost: %+v", requests)
	}
	s := gourmandSessionFixture(t)
	v := gourmandSessionView()
	v.GourmandRequests = requests
	v = s.observeGourmand(v, nil, func([4]int, string) bool { return false })
	if v.GourmandSeat != "1" || len(v.Seats[0].Dishes) != 1 || v.Seats[0].Dishes[0] != "salted_cheese_crackers" {
		t.Fatalf("single dessert not assigned: %+v", v)
	}
	d := PlanReactive(s.stateAt(v, time.Now()))
	if d.Kind != "queue" || d.Dish != "salted_cheese_crackers" {
		t.Fatalf("single dessert not cooked: %+v", d)
	}
}
func TestSecondGourmandPlateKeepsItsOriginalSlotAfterFirstService(t *testing.T) {
	s := gourmandSessionFixture(t)
	s.observeGourmand(gourmandSessionView(), nil, func([4]int, string) bool { return false })
	s.confirmGourmandServing(pendingInput{Gourmand: true, Seat: "1", GourmandSlot: 0, GourmandVisit: 0, Dish: "hot_sour_soup"})
	v := gourmandSessionView()
	v.GourmandRequests = v.GourmandRequests[1:]
	v = s.observeGourmand(v, nil, func([4]int, string) bool { return false })
	if s.Gourmands["1"].Blocked || len(s.stateAt(v, time.Now()).Demands) != 1 || s.stateAt(v, time.Now()).Demands[0].ID != "1/gourmand1" {
		t.Fatal("remaining plate shifted into consumed slot")
	}
}

func TestGourmandSelectsCarriedMatoberrySide(t *testing.T) {
	s := gourmandSessionFixture(t)
	k := s.Kitchens["2"]
	k.Available["tuber_fries"] = true
	k.CookSeconds["tuber_fries"] = 23
	s.Kitchens["2"] = k
	s.Tray = map[string]int{"tuber_fries": 1, "hot_sour_soup": 1}
	v := gourmandSessionView()
	v.Tray = s.Tray
	v.SelectedSlot = 1
	v.TraySlots = []string{"tuber_fries", "hot_sour_soup"}
	text := "Get me a side dish with matoberries."
	v.GourmandRequests = []GourmandRequestView{{Text: text, Requirements: ParseGourmandRequest(text)}}
	v = s.observeGourmand(v, []screenocr.Token{{Text: "1", Score: .99, Box: [4]int{500, 312, 30, 40}}}, func([4]int, string) bool { return true })
	if !v.GourmandRequests[0].Requirements.Parsed || s.Gourmands["1"].Dishes[0] != "tuber_fries" || s.gourmandTraySlot(v) != 0 {
		t.Fatalf("did not select qualifying carried fries: %+v", s.Gourmands)
	}
	if _, ok := s.gourmandServing(v, "hot_sour_soup"); ok {
		t.Fatal("soup is not a side")
	}
}
func TestEquivalentRecordedOCRDoesNotBlockGourmand(t *testing.T) {
	s := gourmandSessionFixture(t)
	v := gourmandSessionView()
	text := "My teeth hurt, so nothing sour, please."
	v.GourmandRequests = []GourmandRequestView{{Text: text, Requirements: ParseGourmandRequest(text)}}
	s.observeGourmand(v, nil, func([4]int, string) bool { return false })
	text = "My teeth hurt, so nothing sourplease."
	v.GourmandRequests = []GourmandRequestView{{Text: text, Requirements: ParseGourmandRequest(text)}}
	s.observeGourmand(v, nil, func([4]int, string) bool { return false })
	if s.Gourmands["1"].Blocked {
		t.Fatal("same constraints with observed punctuation loss blocked the visit")
	}
}

func TestGourmandPairReservesEachCarriedCopyOnce(t *testing.T) {
	s := gourmandSessionFixture(t)
	k := s.Kitchens["2"]
	k.Available["tuber_fries"] = true
	k.CookSeconds["tuber_fries"] = 23
	s.Kitchens["2"] = k
	s.Tray = map[string]int{"tuber_fries": 1, "salted_cheese_crackers": 1}
	order := &gourmandVisit{}
	for i, text := range []string{"My teeth hurt, so nothing sour, please.", "Dessert, but not too sweet. I'm trying to tone my bod."} {
		order.Requests[i] = GourmandRequestView{Text: text, Requirements: ParseGourmandRequest(text)}
	}
	s.assignGourmandDishes(order, time.Now())
	if order.Dishes != [2]string{"tuber_fries", "salted_cheese_crackers"} {
		t.Fatalf("wasted a carried meal or reserved one serving twice: %v", order.Dishes)
	}
	order.Dishes[0] = "salted_cheese_crackers"
	s.assignGourmandDishes(order, time.Now())
	if order.Dishes[0] != "salted_cheese_crackers" {
		t.Fatal("reassigned a committed plate")
	}
}
func TestGourmandETAUsesKitchenQueueTailOnce(t *testing.T) {
	s := gourmandSessionFixture(t)
	now := time.Now()
	s.Jobs = []cookJob{{Kitchen: "2", ExpectedAt: now.Add(10 * time.Second)}, {Kitchen: "2", ExpectedAt: now.Add(20 * time.Second)}}
	eta := s.gourmandMenuETAs(ParseGourmandRequest("Dessert, please."), now)["salted_cheese_crackers"]
	if eta != 35 {
		t.Fatalf("double-counted serial kitchen timers: %v", eta)
	}
}

func TestGourmandNativeShrinkReconcilesBeforeObservation(t *testing.T) {
	s := gourmandSessionFixture(t)
	v := gourmandSessionView()
	s.observeGourmand(v, nil, func([4]int, string) bool { return false })
	s.Pending = &pendingInput{Kind: "serve", Gourmand: true, Seat: "1", GourmandSlot: 0, GourmandVisit: 0, Dish: "hot_sour_soup", Before: 1, IssuedAt: time.Now()}
	v.GourmandRequests = v.GourmandRequests[1:]
	v.Tray = map[string]int{}
	v = s.observeGourmand(v, []screenocr.Token{{Text: "1", Score: .99, Box: [4]int{500, 312, 30, 40}}}, func([4]int, string) bool { return true })
	order := s.Gourmands["1"]
	if order.Blocked || !order.Served[0] || order.Served[1] || !s.Pending.GourmandReconciled || len(v.Seats[0].Dishes) != 1 {
		t.Fatalf("runtime-order acknowledgement lost remaining plate: %+v %+v", order, s.Pending)
	}
}
func TestGourmandAckUsesActualRemainingRule(t *testing.T) {
	s := gourmandSessionFixture(t)
	v := gourmandSessionView()
	texts := []string{"My teeth hurt, so nothing sour, please.", "Dessert, but not too sweet. I'm trying to tone my bod."}
	for i, text := range texts {
		v.GourmandRequests[i] = GourmandRequestView{Text: text, Requirements: ParseGourmandRequest(text)}
	}
	s.observeGourmand(v, nil, func([4]int, string) bool { return false })
	s.Pending = &pendingInput{Kind: "serve", Gourmand: true, Seat: "1", GourmandSlot: 1, GourmandVisit: 0, Dish: "salted_cheese_crackers", Before: 1, IssuedAt: time.Now()}
	v.GourmandRequests = v.GourmandRequests[1:]
	v.Tray = map[string]int{}
	s.observeGourmand(v, []screenocr.Token{{Text: "1", Score: .99, Box: [4]int{500, 312, 30, 40}}}, func([4]int, string) bool { return true })
	if s.Pending.GourmandSlot != 0 || !s.Gourmands["1"].Served[0] || s.Gourmands["1"].Served[1] {
		t.Fatal("internal intended slot overrode the native remaining request")
	}
}
func TestGourmandOtherRowBoardCannotBindWhiteServe(t *testing.T) {
	v := gourmandSessionView()
	v.Seats = []SeatView{{Seat: "4", Phase: "gourmand"}, {Seat: "6", Phase: "gourmand"}}
	v.GourmandBubble = [4]int{450, 320, 250, 100}
	tokens := []screenocr.Token{{Text: "4", Score: .99, Box: [4]int{500, 280, 30, 40}}}
	if seat, verified := gourmandBinding(v, tokens, func([4]int, string) bool { return true }); seat != "" || verified {
		t.Fatal("bound another row's board to the request")
	}
	tokens = append(tokens, screenocr.Token{Text: "6", Score: .99, Box: [4]int{520, 410, 30, 40}})
	if seat, verified := gourmandBinding(v, tokens, func([4]int, string) bool { return true }); seat != "6" || !verified {
		t.Fatal("did not bind the nearby board beneath the bubble")
	}
}
func TestHeldGourmandMealSurvivesZeroPreparableStock(t *testing.T) {
	s := gourmandSessionFixture(t)
	s.Tray = map[string]int{"salted_cheese_crackers": 1}
	s.Kitchens = nil
	order := &gourmandVisit{Count: 1}
	text := "Dessert, please."
	order.Requests[0] = GourmandRequestView{Text: text, Requirements: ParseGourmandRequest(text)}
	s.assignGourmandDishes(order, time.Now())
	if order.Dishes[0] != "salted_cheese_crackers" {
		t.Fatal("held dessert rejected because it cannot be cooked again")
	}
	order = &gourmandVisit{Count: 2}
	for i := range order.Requests {
		order.Requests[i] = GourmandRequestView{Text: text, Requirements: ParseGourmandRequest(text)}
	}
	s.assignGourmandDishes(order, time.Now())
	count := 0
	for _, dish := range order.Dishes {
		if dish == "salted_cheese_crackers" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("one held dessert became two servings with no ingredients")
	}
}

func TestGourmandPopupCanShrinkBeforeTrayAcknowledgement(t *testing.T) {
	s := gourmandSessionFixture(t)
	v := gourmandSessionView()
	s.observeGourmand(v, nil, func([4]int, string) bool { return false })
	s.Pending = &pendingInput{Kind: "serve", Gourmand: true, Seat: "1", GourmandSlot: 0, GourmandVisit: 0, Dish: "hot_sour_soup", Before: 1, IssuedAt: time.Now()}
	v.GourmandRequests = v.GourmandRequests[1:]
	v.Tray = map[string]int{"hot_sour_soup": 1}
	board := []screenocr.Token{{Text: "1", Score: .99, Box: [4]int{500, 312, 30, 40}}}
	s.observeGourmand(v, board, func([4]int, string) bool { return true })
	if s.Gourmands["1"].Blocked || s.Pending.GourmandReconciled {
		t.Fatal("popup-first animation changed lifecycle before tray acknowledgement")
	}
	v.Tray = map[string]int{}
	s.observeGourmand(v, board, func([4]int, string) bool { return true })
	if s.Gourmands["1"].Blocked || !s.Pending.GourmandReconciled || !s.Gourmands["1"].Served[0] {
		t.Fatal("tray-second acknowledgement did not retire the consumed plate")
	}
}
