// Package innkeeping plans bounded inn actions from verified observations.
package innkeeping

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"sort"
	"strings"
)

// Observation contains only states directly supported by current-page OCR.
type Observation struct {
	Phase               string `json:"phase"`
	QueueFull           bool   `json:"queue_full"`
	HelperCollecting    bool   `json:"helper_collecting"`
	CarryingRestriction bool   `json:"carrying_restriction"`
}

// Observe identifies the recorded English inn states; other screens remain unknown.
func Observe(tokens []screenocr.Token) Observation {
	o := Observation{Phase: "unknown"}
	text := ""
	kitchens := 0
	for _, t := range tokens {
		if t.Score < .8 {
			continue
		}
		text += strings.ToLower(t.Text) + "\n"
		if t.Text == "Kitchen 1" || t.Text == "Kitchen 2" {
			kitchens++
		}
		if strings.Contains(t.Text, "Collecting") && t.Box[0] > 1000 && t.Box[1] < 110 {
			o.HelperCollecting = true
		}
	}
	has := func(s string) bool { return strings.Contains(text, s) }
	o.QueueFull = has("cannot cook more. cooking queue is already ful")
	o.HelperCollecting = o.HelperCollecting || has("is helping to collect the dining bills")
	o.CarryingRestriction = has("cannot jump or dash/dodge while holding trays")
	switch {
	case has("session ended") || has("auto-collect remaining income"):
		o.Phase = "ended"
	case has("innkeeping: gourmand") || has("gourmand service:"):
		o.Phase = "tutorial"
	case has("inn closed") || has("quickly serve the dishes and drinks for more income"):
		o.Phase = "closed_serving"
	case has("closing soon"):
		o.Phase = "closing"
	case kitchens >= 2:
		o.Phase = "kitchen"
	case o.QueueFull || (has("current income") && has("patrons seated") && has("dishes sold")):
		o.Phase = "active"
	}
	return o
}

// Order contains exact permitted dish IDs; Gourmand exclusions must be resolved first.
type Order struct {
	Table          string
	Allowed        []string
	WaitingSeconds int
}

// Kitchen contains an observed free slot count and already reserved output.
type Kitchen struct {
	ID               string
	FreeSlots        int
	CapacityVerified bool
	Queued           map[string]int
}

// Recipe contains observed ingredient availability and completion time.
type Recipe struct {
	Kitchen             string
	ID                  string
	IngredientsVerified bool
	Available           bool
	CookSeconds         int
}

// State keeps unknown stock, requirements and travel estimates explicitly unverified.
type State struct {
	Observation        Observation    `json:"observation"`
	Orders             []Order        `json:"orders"`
	Kitchens           []Kitchen      `json:"kitchens"`
	Recipes            []Recipe       `json:"recipes"`
	Tray               map[string]int `json:"tray"`
	Ready              map[string]int `json:"ready"`
	OrdersVerified     bool           `json:"orders_verified"`
	StockVerified      bool           `json:"stock_verified"`
	TrayCapacity       int            `json:"tray_capacity"`
	RemainingSeconds   int            `json:"remaining_seconds"`
	DeliverySeconds    int            `json:"delivery_seconds"`
	TravelTimeVerified bool           `json:"travel_time_verified"`
	NavigationVerified bool           `json:"navigation_verified"`
	Bills              []string       `json:"bills"`
}

// Decision is a recommendation, never permission to issue an input blindly.
type Decision struct {
	Kind             string `json:"kind"`
	Target           string `json:"target,omitempty"`
	Dish             string `json:"dish,omitempty"`
	Reason           string `json:"reason"`
	BlockedBy        string `json:"blocked_by,omitempty"`
	ExecutionEnabled bool   `json:"execution_enabled"`
}

// Plan prioritizes held-food delivery, needed ready food, demand-driven cooking, then bills.
// It reserves existing stock across all orders and never speculates from elapsed time alone.
func Plan(s State) Decision {
	d := Decision{}
	switch s.Observation.Phase {
	case "ended":
		return Decision{Kind: "finish", Reason: "Session ended; remaining income is collected by the game."}
	case "unknown":
		return Decision{Kind: "inspect", Reason: "No verified inn session screen detected."}
	case "tutorial":
		return Decision{Kind: "inspect_tutorial", Reason: "Tutorial blocks the session; inspect it before continuing."}
	}
	if !s.OrdersVerified || !s.StockVerified {
		reason := "Read dish icons, requests, held food and ready dishes before selecting an action."
		if s.Observation.QueueFull {
			reason = "Current kitchen queue is full. Do not enqueue again; inspect ready food and delivery demand."
		}
		return Decision{Kind: "inspect_orders_and_stock", Reason: reason, BlockedBy: "dish icon and inventory recognition not mapped"}
	}
	for _, stock := range []map[string]int{s.Tray, s.Ready} {
		for _, n := range stock {
			if n < 0 {
				return Decision{Kind: "inspect", Reason: "Invalid stock count."}
			}
		}
	}
	for _, k := range s.Kitchens {
		if k.FreeSlots < 0 {
			return Decision{Kind: "inspect", Reason: "Invalid kitchen capacity."}
		}
		for _, n := range k.Queued {
			if n < 0 {
				return Decision{Kind: "inspect", Reason: "Invalid queued stock."}
			}
		}
	}
	orders := append([]Order(nil), s.Orders...)
	sort.SliceStable(orders, func(i, j int) bool { return orders[i].WaitingSeconds > orders[j].WaitingSeconds })
	for _, order := range orders {
		for _, dish := range order.Allowed {
			if s.Tray[dish] > 0 {
				return movement(s, Decision{Kind: "serve", Target: order.Table, Dish: dish, Reason: "Deliver held food to the longest-waiting compatible order."})
			}
		}
	}
	held := 0
	for _, n := range s.Tray {
		if n < 0 {
			return Decision{Kind: "inspect", Reason: "Invalid held stock count."}
		}
		held += n
	}
	if held < s.TrayCapacity {
		for _, order := range orders {
			for _, dish := range order.Allowed {
				if s.Ready[dish] > 0 {
					return movement(s, Decision{Kind: "pickup", Target: order.Table, Dish: dish, Reason: "Collect a ready dish that satisfies an existing order."})
				}
			}
		}
	}
	// Existing ready/held/queued units each cover at most one request.
	reserved := map[string]int{}
	for id, n := range s.Ready {
		reserved[id] += n
	}
	for id, n := range s.Tray {
		reserved[id] += n
	}
	for _, k := range s.Kitchens {
		for id, n := range k.Queued {
			reserved[id] += n
		}
	}
	for _, order := range orders {
		covered := false
		for _, dish := range order.Allowed {
			if reserved[dish] > 0 {
				reserved[dish]--
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		for _, dish := range order.Allowed {
			for _, recipe := range s.Recipes {
				if recipe.ID != dish || !recipe.IngredientsVerified || !recipe.Available {
					continue
				}
				if !s.TravelTimeVerified || recipe.CookSeconds <= 0 || s.RemainingSeconds <= recipe.CookSeconds+s.DeliverySeconds {
					continue
				}
				for _, kitchen := range s.Kitchens {
					if !s.Observation.QueueFull && kitchen.CapacityVerified && kitchen.FreeSlots > 0 && recipe.Kitchen == kitchen.ID {
						return Decision{Kind: "queue", Target: kitchen.ID, Dish: dish, Reason: "Uncovered confirmed demand fits the observed cook-and-delivery deadline.", BlockedBy: "recipe click and queue-change verification adapter pending"}
					}
				}
			}
		}
	}
	if !s.Observation.HelperCollecting && len(s.Bills) > 0 {
		return movement(s, Decision{Kind: "collect_bill", Target: s.Bills[0], Reason: "No immediate food action; helper is not already collecting."})
	}
	d.Kind = "wait_for_state_change"
	d.Reason = "No safe unreserved action. Preserve kitchen capacity and avoid late speculative cooking."
	return d
}

func movement(s State, d Decision) Decision {
	if !s.NavigationVerified {
		d.BlockedBy = "counter/table navigation requires mapping"
	} else {
		d.BlockedBy = "live execution adapter pending"
	}
	return d
}
