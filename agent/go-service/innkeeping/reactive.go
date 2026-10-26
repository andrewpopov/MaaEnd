package innkeeping

import "strings"

// ChefKitchen keeps capacity, queued work and specialties separate for each cook.
type ChefKitchen struct {
	ID          string             `json:"id"`
	BatchIssued int                `json:"batch_issued,omitempty"`
	Chef        string             `json:"chef"`
	Chefs       []string           `json:"chefs"`
	Capacity    int                `json:"capacity"`
	Queued      map[string]int     `json:"queued"`
	BusySeconds float64            `json:"busy_seconds"`
	Specialties []string           `json:"specialties"`
	CookSeconds map[string]float64 `json:"cook_seconds"`
	Available   map[string]bool    `json:"available"`
	Verified    bool               `json:"verified"`
}

// Requirements represents every verified positive and negative Gourmand condition.
type Requirements struct {
	Parsed             bool     `json:"parsed"`
	IncludeTypes       []string `json:"include_types"`
	ExcludeTypes       []string `json:"exclude_types"`
	IncludeIngredients []string `json:"include_ingredients"`
	ExcludeIngredients []string `json:"exclude_ingredients"`
	IncludeTraits      []string `json:"include_traits"`
	AnyTraits          []string `json:"any_traits,omitempty"`
	ExcludeTraits      []string `json:"exclude_traits"`
}

// DishDefinition stores facts separately from observed kitchen speed and stock.
type DishDefinition struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	Category            string         `json:"category"`
	Ingredients         map[string]int `json:"ingredients"`
	IngredientsComplete bool           `json:"ingredients_complete"`
	Traits              []string       `json:"traits"`
	TraitsComplete      bool           `json:"traits_complete"`
	BaseCookSeconds     float64        `json:"base_cook_seconds"`
}

func includes(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// Accepts never treats an unparsed Gourmand request as permission to serve anything.
func (r Requirements) Accepts(d DishDefinition) bool {
	if !r.Parsed {
		return false
	}
	if len(r.IncludeTypes) > 0 && !includes(r.IncludeTypes, d.Category) {
		return false
	}
	if includes(r.ExcludeTypes, d.Category) {
		return false
	}
	if (len(r.IncludeIngredients) > 0 || len(r.ExcludeIngredients) > 0) && !d.IngredientsComplete {
		return false
	}
	if len(r.ExcludeTraits) > 0 && !d.TraitsComplete {
		return false
	}
	hasIngredient := func(want string) bool {
		for name, n := range d.Ingredients {
			if n > 0 && ingredientFamily(name) == ingredientFamily(want) {
				return true
			}
		}
		return false
	}
	for _, v := range r.IncludeIngredients {
		if !hasIngredient(v) {
			return false
		}
	}
	for _, v := range r.ExcludeIngredients {
		if hasIngredient(v) {
			return false
		}
	}
	for _, v := range r.IncludeTraits {
		if !includes(d.Traits, v) {
			return false
		}
	}
	if len(r.AnyTraits) > 0 {
		matched := false
		for _, trait := range r.AnyTraits {
			matched = matched || includes(d.Traits, trait)
		}
		if !matched {
			return false
		}
	}
	for _, v := range r.ExcludeTraits {
		if includes(d.Traits, v) {
			return false
		}
	}
	return true
}

// ingredientFamily makes a rice exclusion cover both ordinary and [A] rice.
func ingredientFamily(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSpace(strings.TrimSuffix(s, "[a]"))
	switch s {
	case "rice", "rice grains", "amber rice":
		return "rice"
	case "fish", "fins", "fin fillet":
		return "fin"
	case "matoberries":
		return "matoberry"
	case "sourberries":
		return "sourberry"
	case "eggs":
		return "egg"
	case "mushroom", "mushrooms":
		return "relishroom"
	}
	return s
}

// Demand identifies one requested item, not all items at a table as alternatives.
type Demand struct {
	ID             string       `json:"id"`
	Seat           string       `json:"seat"`
	Allowed        []string     `json:"allowed"`
	WaitingSeconds float64      `json:"waiting_seconds"`
	Gourmand       bool         `json:"gourmand"`
	Requirements   Requirements `json:"requirements"`
}

// ReactiveState contains confirmed stock only. Queue slots are counted independently.
type ReactiveState struct {
	Phase              string           `json:"phase"`
	HoldKitchen        bool             `json:"hold_kitchen,omitempty"`
	CurrentKitchen     string           `json:"current_kitchen,omitempty"`
	CurrentKitchenIdle bool             `json:"current_kitchen_idle,omitempty"`
	Demands            []Demand         `json:"demands"`
	Kitchens           []ChefKitchen    `json:"kitchens"`
	Dishes             []DishDefinition `json:"dishes"`
	Tray               map[string]int   `json:"tray"`
	Ready              map[string]int   `json:"ready"`
	CounterPending     map[string]int   `json:"counter_pending,omitempty"`
	CollectionDue      bool             `json:"collection_due,omitempty"`
	TrayCapacity       int              `json:"tray_capacity"`
	RemainingSeconds   float64          `json:"remaining_seconds"`
	Verified           bool             `json:"verified"`
}

// ReactiveDecision contains a single requested action for the Pipeline to execute and verify.
type ReactiveDecision struct {
	Kind            string  `json:"kind"`
	Demand          string  `json:"demand,omitempty"`
	Seat            string  `json:"seat,omitempty"`
	Dish            string  `json:"dish,omitempty"`
	Kitchen         string  `json:"kitchen,omitempty"`
	Chef            string  `json:"chef,omitempty"`
	ExpectedSeconds float64 `json:"expected_seconds,omitempty"`
	Reason          string  `json:"reason"`
}

// PlanReactive serves visible demand without pre-cooking or guessed capacities.
func PlanReactive(s ReactiveState) ReactiveDecision {
	if s.Phase == "ended" {
		return ReactiveDecision{Kind: "finish", Reason: "Session ended."}
	}
	if !s.Verified {
		return ReactiveDecision{Kind: "inspect", Reason: "Orders, tray and both queues must be observed."}
	}
	demands := completionDemands(s)
	allowed := func(o Demand, id string) bool {
		if !includes(o.Allowed, id) {
			return false
		}
		if !o.Gourmand {
			return true
		}
		for _, d := range s.Dishes {
			if d.ID == id {
				return o.Requirements.Accepts(d)
			}
		}
		return false
	}
	held := 0
	for _, n := range s.Tray {
		if n < 0 {
			return ReactiveDecision{Kind: "inspect", Reason: "Invalid tray count."}
		}
		held += n
	}
	if held > s.TrayCapacity {
		return ReactiveDecision{Kind: "inspect", Reason: "Tray capacity exceeded."}
	}
	// Carried, ready or due food ends the kitchen visit. Fill idle capacity
	// before that point; refilling after each short cook can strand meals.
	delivery := ReactiveDecision{}
	{
		for _, o := range demands {
			for _, id := range o.Allowed {
				if allowed(o, id) && s.Tray[id] > 0 {
					return ReactiveDecision{Kind: "serve", Demand: o.ID, Seat: o.Seat, Dish: id, Reason: "Serve food toward a complete customer meal."}
				}
			}
		}
		if held < s.TrayCapacity && !(s.Phase == "kitchen" && s.HoldKitchen) {
			for _, o := range demands {
				for _, id := range o.Allowed {
					if allowed(o, id) && s.Ready[id] > 0 {
						if delivery.Kind == "" {
							delivery = ReactiveDecision{Kind: "pickup", Demand: o.ID, Seat: o.Seat, Dish: id, Reason: "Ready food covers current demand."}
						}
					}
				}
			}
		}
	}
	if delivery.Kind != "" {
		return delivery
	}
	if s.Phase == "kitchen" && held > 0 {
		return ReactiveDecision{Kind: "close", Reason: "Leave the menu with carried food before planning more cooking."}
	}
	if s.Phase == "kitchen" && s.CollectionDue && !s.HoldKitchen {
		return ReactiveDecision{Kind: "collect", Reason: "A needed dish is due for counter inspection; leave before extending cooking work."}
	}
	reserved := map[string]int{}
	for id, n := range s.Tray {
		reserved[id] += n
	}
	for id, n := range s.Ready {
		if n < 0 {
			return ReactiveDecision{Kind: "inspect", Reason: "Invalid ready count."}
		}
		reserved[id] += n
	}
	for id, n := range s.CounterPending {
		if n < 0 {
			return ReactiveDecision{Kind: "inspect", Reason: "Invalid expected counter count."}
		}
		reserved[id] += n
	}
	for _, k := range s.Kitchens {
		for id, n := range k.Queued {
			if n < 0 {
				return ReactiveDecision{Kind: "inspect", Reason: "Invalid queue count."}
			}
			reserved[id] += n
		}
	}
	firstQueue := ReactiveDecision{}
	currentSpecialist := ReactiveDecision{}
	for _, o := range demands {
		covered := false
		for _, id := range o.Allowed {
			if allowed(o, id) && reserved[id] > 0 {
				reserved[id]--
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		best := ReactiveDecision{Kind: "wait", Reason: "No eligible kitchen for visible demand."}
		bestSpecialty := false
		for _, id := range o.Allowed {
			if !allowed(o, id) {
				continue
			}
			category := ""
			for _, d := range s.Dishes {
				if d.ID == id {
					category = d.Category
				}
			}
			for _, k := range s.Kitchens {
				occupied := 0
				for _, n := range k.Queued {
					occupied += n
				}
				if s.Phase == "kitchen" && s.CurrentKitchen == k.ID && s.CurrentKitchenIdle {
					// Completed counter food remains reserved above. A freshly
					// observed empty cooking queue still has room for useful work.
					occupied, k.BusySeconds = 0, 0
				}
				seconds, known := k.CookSeconds[id]
				if !k.Verified || k.Capacity <= 0 || occupied >= k.Capacity || max(occupied, k.BatchIssued) >= 2 || !k.Available[id] || !known || seconds <= 0 {
					continue
				}
				eta := k.BusySeconds + seconds
				idle := occupied == 0 && k.BusySeconds <= 0
				// A select/acknowledgement costs about three seconds. Switching
				// between working kitchens also incurs the return trip; fill a
				// useful current batch unless completion improves by that cost.
				if s.Phase == "kitchen" && s.CurrentKitchen != "" && s.CurrentKitchen != k.ID {
					eta += 3
					if !idle {
						eta += 3
					}
				}
				if s.RemainingSeconds > 0 && eta >= s.RemainingSeconds {
					continue
				}
				specialty := includes(k.Specialties, category)
				if best.Kind == "wait" || eta < best.ExpectedSeconds || (eta == best.ExpectedSeconds && specialty && !bestSpecialty) {
					best = ReactiveDecision{Kind: "queue", Demand: o.ID, Seat: o.Seat, Dish: id, Kitchen: k.ID, Chef: k.Chef, ExpectedSeconds: eta, Reason: "Minimize queue completion including kitchen selection; prefer observed specialty at equal finish time."}
					bestSpecialty = specialty
				}
			}
		}
		if best.Kind == "queue" {
			if firstQueue.Kind == "" {
				firstQueue = best
			}
			// Inspect all uncovered dishes before leaving this kitchen. Give
			// its specialist useful work first, provided that kitchen also
			// wins the finish-time comparison for that dish.
			if s.Phase == "kitchen" && best.Kitchen == s.CurrentKitchen && bestSpecialty && (currentSpecialist.Kind == "" || best.ExpectedSeconds < currentSpecialist.ExpectedSeconds) {
				currentSpecialist = best
			}
		}
	}
	if currentSpecialist.Kind != "" {
		currentSpecialist.Reason = "Queue the current cook's observed specialty before switching kitchens for other uncovered dishes."
		return currentSpecialist
	}
	if firstQueue.Kind != "" {
		return firstQueue
	}
	return ReactiveDecision{Kind: "wait", Reason: "Existing food and queued work cover demand, or required facts are unknown."}
}
