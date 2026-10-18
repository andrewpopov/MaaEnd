package innkeeping

import (
	"bufio"
	"encoding/json"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"os"
	"testing"
)

func fixture() State {
	return State{Observation: Observation{Phase: "active"}, OrdersVerified: true, StockVerified: true, Orders: []Order{{Table: "1", Allowed: []string{"side"}, WaitingSeconds: 20}}, Kitchens: []Kitchen{{ID: "1", FreeSlots: 1, CapacityVerified: true}}, Recipes: []Recipe{{ID: "side", Kitchen: "1", IngredientsVerified: true, Available: true, CookSeconds: 14}}, Tray: map[string]int{}, Ready: map[string]int{}, TrayCapacity: 2, RemainingSeconds: 90, DeliverySeconds: 8, TravelTimeVerified: true}
}
func TestPolicies(t *testing.T) {
	cases := []struct {
		name string
		edit func(*State)
		want string
	}{
		{"serve first", func(s *State) { s.Tray["side"] = 1 }, "serve"},
		{"pickup first", func(s *State) { s.Ready["side"] = 1 }, "pickup"},
		{"known demand", func(s *State) {}, "queue"},
		{"full queue", func(s *State) { s.Observation.QueueFull = true }, "wait_for_state_change"},
		{"reserved output", func(s *State) { s.Kitchens[0].Queued = map[string]int{"side": 1} }, "wait_for_state_change"},
		{"unknown capacity", func(s *State) { s.Kitchens[0].CapacityVerified = false }, "wait_for_state_change"},
		{"unknown ingredients", func(s *State) { s.Recipes[0].IngredientsVerified = false }, "wait_for_state_change"},
		{"wrong chef estimate", func(s *State) { s.Recipes[0].Kitchen = "2" }, "wait_for_state_change"},
		{"too late", func(s *State) { s.RemainingSeconds = 21 }, "wait_for_state_change"},
		{"helper busy", func(s *State) { s.Kitchens = nil; s.Bills = []string{"2"}; s.Observation.HelperCollecting = true }, "wait_for_state_change"},
		{"collect bill", func(s *State) { s.Kitchens = nil; s.Bills = []string{"2"} }, "collect_bill"},
		{"negative stock", func(s *State) { s.Ready["side"] = -1 }, "inspect"},
		{"unknown order", func(s *State) { s.OrdersVerified = false }, "inspect_orders_and_stock"},
		{"finish", func(s *State) { s.Observation.Phase = "ended"; s.OrdersVerified = false }, "finish"},
		{"excluded dish", func(s *State) { s.Recipes[0].ID = "entree" }, "wait_for_state_change"},
		{"no speculation", func(s *State) { s.Orders = nil }, "wait_for_state_change"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			tc.edit(&s)
			d := Plan(s)
			if d.Kind != tc.want {
				t.Fatalf("got %s: %s", d.Kind, d.Reason)
			}
			if d.ExecutionEnabled {
				t.Fatal("inputs enabled")
			}
		})
	}
}
func TestOldestOrder(t *testing.T) {
	s := fixture()
	s.Tray["side"] = 1
	s.Orders = append(s.Orders, Order{Table: "2", Allowed: []string{"side"}, WaitingSeconds: 40})
	if Plan(s).Target != "2" {
		t.Fatal("wrong priority")
	}
}
func TestQueuedReservation(t *testing.T) {
	s := fixture()
	s.Kitchens[0].Queued = map[string]int{"side": 1}
	s.Orders = append(s.Orders, s.Orders[0])
	if Plan(s).Kind != "queue" {
		t.Fatal("one queued unit covered two orders")
	}
}
func TestRecordedRound(t *testing.T) {
	p := os.Getenv("MAAEND_INN_OCR_REPLAY")
	if p == "" {
		t.Skip("optional archived round")
	}
	f, e := os.Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 65536), 1024*1024)
	counts := map[string]int{}
	total := 0
	for scanner.Scan() {
		var r struct {
			Text []screenocr.Token `json:"text"`
		}
		if e := json.Unmarshal(scanner.Bytes(), &r); e != nil {
			t.Fatal(e)
		}
		counts[Observe(r.Text).Phase]++
		total++
	}
	if scanner.Err() != nil {
		t.Fatal(scanner.Err())
	}
	if total != 564 {
		t.Fatalf("coverage %d", total)
	}
	for _, p := range []string{"active", "kitchen", "closing", "closed_serving", "ended", "tutorial", "unknown"} {
		if counts[p] == 0 {
			t.Fatalf("missing %s", p)
		}
	}
	t.Log(counts)
}
