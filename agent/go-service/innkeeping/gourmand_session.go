package innkeeping

import (
	"fmt"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"reflect"
	"strings"
	"time"
)

type gourmandVisit struct {
	Count    int                    `json:"count"`
	Blocked  bool                   `json:"blocked,omitempty"`
	Visit    int                    `json:"visit"`
	Requests [2]GourmandRequestView `json:"requests"`
	Dishes   [2]string              `json:"dishes"`
	Served   [2]bool                `json:"served"`
}

// Nearby request text belongs to exactly one diner. A physical board resolves
// multiple gold cards; a sole gold card is sufficient only with the popup visible.
func gourmandBinding(v LiveView, tokens []screenocr.Token, verify func([4]int, string) bool) (string, bool) {
	gold := map[string]bool{}
	for _, seat := range v.Seats {
		if seat.Phase == "gourmand" {
			gold[seat.Seat] = true
		}
	}
	found := ""
	for _, t := range tokens {
		id := strings.TrimSpace(t.Text)
		if !gold[id] || !validSeatMarker(t.Box) || !gourmandBoardNearBubble(v.GourmandBubble, t.Box) || !verify(t.Box, id) {
			continue
		}
		if found != "" && found != id {
			return "", false
		}
		found = id
	}
	if found != "" {
		return found, true
	}
	if len(v.GourmandRequests) > 0 && len(gold) == 1 {
		for id := range gold {
			return id, false
		}
	}
	return "", false
}

func gourmandSeat(v LiveView, tokens []screenocr.Token, verify func([4]int, string) bool) string {
	seat, _ := gourmandBinding(v, tokens, verify)
	return seat
}

// The measured serving boards sit below the request bubble, near its tail.
// A recognizable board from the next row cannot identify the active request.
func gourmandBoardNearBubble(bubble, board [4]int) bool {
	if bubble[2] <= 0 || bubble[3] <= 0 {
		return false
	}
	dx := board[0] + board[2]/2 - (bubble[0] + bubble[2]/2)
	dy := board[1] + board[3]/2 - (bubble[1] + bubble[3])
	return abs(dx) <= 140 && dy >= -20 && dy <= 110
}

func (s *liveSession) observeGourmand(v LiveView, tokens []screenocr.Token, verify func([4]int, string) bool) LiveView {
	if s.Gourmands == nil {
		s.Gourmands = map[string]*gourmandVisit{}
	}
	tokens = append(tokens, v.GourmandBoards...)
	if len(v.GourmandRequests) > 0 || v.Prompt == "serve" {
		v.GourmandSeat, v.GourmandTargetVerified = gourmandBinding(v, tokens, verify)
	}
	if p := s.Pending; p != nil && p.Gourmand && p.Kind == "serve" && !p.IssuedAt.IsZero() {
		if !s.reconcileGourmandServe(v, p) {
			return s.gourmandView(v)
		}
	}
	for _, seat := range v.Seats {
		i := seatIndex(seat.Seat)
		if order := s.Gourmands[seat.Seat]; order != nil && (i < 0 || order.Visit != s.Visit[i] || seat.Phase == "dining" || seat.Phase == "bill_ready" || seat.Phase == "ordering" || seat.Phase == "waiting") {
			delete(s.Gourmands, seat.Seat)
		}
	}
	if len(v.GourmandRequests) > 0 && len(v.GourmandRequests) <= 2 && v.GourmandSeat != "" {
		i := seatIndex(v.GourmandSeat)
		order := s.Gourmands[v.GourmandSeat]
		if order == nil {
			order = &gourmandVisit{Visit: s.Visit[i], Count: len(v.GourmandRequests)}
			s.Gourmands[v.GourmandSeat] = order
		}
		if order.Blocked {
			v.GourmandSeat = ""
			return s.gourmandView(v)
		}
		offset := 0
		if order.Count == 2 && len(v.GourmandRequests) == 1 {
			if order.Served[0] && !order.Served[1] {
				offset = 1
			} else if !order.Served[0] && order.Served[1] {
				offset = 0
			} else {
				order.Blocked = true
				v.GourmandSeat = ""
				return s.gourmandView(v)
			}
		}
		for observed, request := range v.GourmandRequests {
			slot := observed + offset
			parsed := ParseGourmandRequest(request.Text)
			if !parsed.Parsed || !reflect.DeepEqual(parsed, request.Requirements) {
				order.Blocked = true
				v.GourmandSeat = ""
				return s.gourmandView(v)
			}
			if order.Requests[slot].Text != "" && !reflect.DeepEqual(order.Requests[slot].Requirements, request.Requirements) {
				// A changed sentence is new evidence, not permission to reuse a
				// partially served order. Reacquire the whole visit explicitly.
				order.Blocked = true
				v.GourmandSeat = ""
				return v
			}
			order.Requests[slot] = request
		}
	}
	return s.gourmandView(v)
}

func seatIndex(seat string) int {
	if len(seat) == 1 && seat >= "1" && seat <= "6" {
		return int(seat[0] - '1')
	}
	return -1
}

// Choose one concrete menu recipe per serving. All stock/queue helpers then
// reserve copies normally rather than interpreting every matching dish as owed.
func (s *liveSession) gourmandMenuETAs(r Requirements, now time.Time) map[string]float64 {
	options := map[string]float64{}
	for _, dish := range s.Catalog {
		if !r.Accepts(dish) {
			continue
		}
		if s.Tray[dish.ID] > 0 {
			options[dish.ID] = 1e9
		}
		for _, k := range s.Kitchens {
			seconds := k.CookSeconds[dish.ID]
			if !k.Verified || !k.Available[dish.ID] || seconds <= 0 {
				continue
			}
			tail := 0.0
			for _, job := range s.Jobs {
				if job.Kitchen == k.ID {
					tail = max(tail, job.ExpectedAt.Sub(now).Seconds())
				}
			}
			eta := seconds + tail
			if old, ok := options[dish.ID]; !ok || eta < old {
				options[dish.ID] = eta
			}
		}
	}
	return options
}

// Solve both plates together. One carried cracker cannot simultaneously cover
// a flexible request and a dessert-only request when another carried dish fits.
// Existing assignments remain stable once cooking or delivery has begun.
func (s *liveSession) assignGourmandDishes(order *gourmandVisit, now time.Time) {
	choices := order.Dishes
	used := map[string]int{}
	var pending []int
	options := [2]map[string]float64{}
	for slot, request := range order.Requests {
		if order.Served[slot] {
			continue
		}
		if choices[slot] != "" {
			used[choices[slot]]++
			continue
		}
		options[slot] = s.gourmandMenuETAs(request.Requirements, now)
		if len(options[slot]) > 0 {
			pending = append(pending, slot)
		}
	}
	best := choices
	bestScore := 1e9
	bestKey := ""
	var search func(int, float64)
	search = func(index int, score float64) {
		if index == len(pending) {
			key := choices[0] + "/" + choices[1]
			if score < bestScore || score == bestScore && (bestKey == "" || key < bestKey) {
				best, bestScore, bestKey = choices, score, key
			}
			return
		}
		slot := pending[index]
		for dish, eta := range options[slot] {
			if used[dish] < s.Tray[dish] {
				eta = 0
			} else if eta >= 1e9 {
				continue
			}
			used[dish]++
			choices[slot] = dish
			search(index+1, score+eta)
			used[dish]--
		}
		choices[slot] = ""
		search(index+1, score+1e6)
	}
	search(0, 0)
	order.Dishes = best
}

func (s *liveSession) gourmandView(v LiveView) LiveView {
	v.Seats = append([]SeatView(nil), v.Seats...)
	for i, seat := range v.Seats {
		if seat.Phase == "gourmand" {
			v.Seats[i].Dishes = nil
		}
		order := s.Gourmands[seat.Seat]
		index := seatIndex(seat.Seat)
		if seat.Phase != "gourmand" || order == nil || order.Blocked || index < 0 || order.Visit != s.Visit[index] {
			continue
		}
		v.Seats[i].Dishes = nil
		s.assignGourmandDishes(order, time.Now())
		for slot := range order.Requests {
			if order.Served[slot] {
				continue
			}
			if order.Dishes[slot] != "" {
				v.Seats[i].Dishes = append(v.Seats[i].Dishes, order.Dishes[slot])
			}
		}
	}
	return v
}

func (s *liveSession) gourmandDemands(seat string) []Demand {
	order := s.Gourmands[seat]
	index := seatIndex(seat)
	if order == nil || order.Blocked || index < 0 || order.Visit != s.Visit[index] {
		return nil
	}
	var result []Demand
	for slot, request := range order.Requests {
		if !order.Served[slot] && order.Dishes[slot] != "" {
			result = append(result, Demand{ID: fmt.Sprintf("%s/gourmand%d", seat, slot), Seat: seat, Allowed: []string{order.Dishes[slot]}, Gourmand: true, Requirements: request.Requirements})
		}
	}
	return result
}

func (s *liveSession) gourmandServing(v LiveView, dish string) (int, bool) {
	order := s.Gourmands[v.GourmandSeat]
	index := seatIndex(v.GourmandSeat)
	if order == nil || order.Blocked || index < 0 || order.Visit != s.Visit[index] || len(v.GourmandRequests) == 0 || !v.GourmandTargetVerified || !v.GourmandTextComplete {
		return 0, false
	}
	for slot := range order.Dishes {
		if !order.Served[slot] && order.Dishes[slot] == dish {
			return slot, true
		}
	}
	return 0, false
}

func (s *liveSession) confirmGourmandServing(p pendingInput) {
	if !p.Gourmand {
		return
	}
	order := s.Gourmands[p.Seat]
	index := seatIndex(p.Seat)
	if order != nil && !order.Blocked && index >= 0 && s.Visit[index] == p.GourmandVisit && order.Visit == p.GourmandVisit && p.GourmandSlot >= 0 && p.GourmandSlot < 2 && order.Dishes[p.GourmandSlot] == p.Dish {
		order.Served[p.GourmandSlot] = true
	}
}

// A verified gourmand request selects the matching carried meal directly;
// ordinary world bubbles can belong to a different nearby table.
func (s *liveSession) gourmandTraySlot(v LiveView) int {
	for slot, dish := range v.TraySlots {
		if slot != v.SelectedSlot && dish != "" {
			if _, ok := s.gourmandServing(v, dish); ok {
				return slot
			}
		}
	}
	return -1
}

// Reconcile the native remaining request BEFORE interpreting a shrinking popup.
// Tray disappearance alone does not identify which of two overlapping rules won.
func (s *liveSession) reconcileGourmandServe(v LiveView, p *pendingInput) bool {
	if p == nil || !p.Gourmand || p.Kind != "serve" || p.IssuedAt.IsZero() || v.Phase == "kitchen" || v.Tray[p.Dish] >= p.Before {
		return false
	}
	if p.GourmandReconciled {
		return true
	}
	order := s.Gourmands[p.Seat]
	index := seatIndex(p.Seat)
	if order == nil || order.Blocked || index < 0 || order.Visit != p.GourmandVisit || s.Visit[index] != p.GourmandVisit {
		return false
	}
	var unserved []int
	for slot := 0; slot < order.Count; slot++ {
		if !order.Served[slot] {
			unserved = append(unserved, slot)
		}
	}
	consumed := -1
	if len(unserved) == 1 {
		for _, seat := range v.Seats {
			if seat.Seat == p.Seat && (seat.Phase == "dining" || seat.Phase == "bill_ready") {
				consumed = unserved[0]
			}
		}
	} else if len(unserved) == 2 && v.GourmandSeat == p.Seat && v.GourmandTargetVerified && v.GourmandTextComplete && len(v.GourmandRequests) == 1 {
		remaining := v.GourmandRequests[0].Requirements
		a := reflect.DeepEqual(remaining, order.Requests[unserved[0]].Requirements)
		b := reflect.DeepEqual(remaining, order.Requests[unserved[1]].Requirements)
		if a && b {
			consumed = unserved[0]
		} else if a {
			consumed = unserved[1]
		} else if b {
			consumed = unserved[0]
		}
	}
	if consumed < 0 {
		return false
	}
	qualifies := false
	for _, dish := range s.Catalog {
		if dish.ID == p.Dish {
			qualifies = order.Requests[consumed].Requirements.Accepts(dish)
			break
		}
	}
	if !qualifies {
		return false
	}
	order.Served[consumed] = true
	p.GourmandSlot = consumed
	p.GourmandReconciled = true
	return true
}
