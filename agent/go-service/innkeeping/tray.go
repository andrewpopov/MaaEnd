package innkeeping

import "time"

const defaultTrayCapacity = 2

func (s *liveSession) trayCapacity() int {
	if s.TrayCapacity >= 2 && s.TrayCapacity <= 5 {
		return s.TrayCapacity
	}
	return defaultTrayCapacity
}

func trayCount(tray map[string]int) int {
	count := 0
	for _, n := range tray {
		count += n
	}
	return count
}

// confirmedPickup reconciles the exact one-copy tray gain. A just-finished dish
// can replace the highlighted pickup between the final image and input release.
// Only known, tracked food still needed by a diner can acknowledge that race.
func confirmedPickup(p pendingInput, v LiveView, jobs []cookJob) string {
	if p.TrayBefore == nil || v.Phase == "kitchen" || len(v.Unknown) > 0 || trayCount(v.Tray) != trayCount(p.TrayBefore)+1 {
		return ""
	}
	actual := ""
	for dish, before := range p.TrayBefore {
		if v.Tray[dish] < before {
			return ""
		}
	}
	for dish, after := range v.Tray {
		delta := after - p.TrayBefore[dish]
		if delta == 0 {
			continue
		}
		if delta != 1 || actual != "" {
			return ""
		}
		actual = dish
	}
	beforeView := v
	beforeView.Tray = p.TrayBefore
	if !neededPickup(beforeView, actual) {
		return ""
	}
	for _, job := range jobs {
		if job.Dish == actual {
			return actual
		}
	}
	return ""
}

func selectedTrayDish(v LiveView) string {
	if v.SelectedSlot < 0 || v.SelectedSlot >= len(v.TraySlots) {
		return ""
	}
	return v.TraySlots[v.SelectedSlot]
}

func neededPickup(v LiveView, dish string) bool {
	limit := v.TrayCapacity
	if limit < 2 || limit > 5 {
		limit = defaultTrayCapacity
	}
	if dish == "" || trayCount(v.Tray) >= limit {
		return false
	}
	needed := 0
	for _, seat := range v.Seats {
		for _, requested := range seat.Dishes {
			if requested == dish {
				needed++
			}
		}
	}
	return needed > v.Tray[dish]
}

// readyForTray reserves carried copies before choosing another ready pickup.
func readyForTray(jobs []cookJob, v LiveView, now time.Time) map[string]bool {
	ready := collectibleDishes(jobs, v.Seats, now)
	for dish := range ready {
		if !neededPickup(v, dish) {
			delete(ready, dish)
		}
	}
	return ready
}

func (s *liveSession) collectingBatch(v LiveView, now time.Time) bool {
	return trayCount(v.Tray) > 0 && trayCount(v.Tray) < s.trayCapacity() && !s.Navigation.Departed &&
		(s.Navigation.Goal == "counter" || atPickupCounter(v)) && len(readyForTray(s.Jobs, v, now)) > 0
}

// nextTraySlot tests each carried dish at most once at an observed Serve prompt.
// Every selection is verified and only a fresh green prompt permits serving.
func nextTraySlot(v LiveView, tried [5]bool) int {
	for slot, dish := range v.TraySlots {
		if slot < len(tried) && dish != "" && slot != v.SelectedSlot && !tried[slot] {
			return slot
		}
	}
	return -1
}

// nextDinerTraySlot selects for the same nearest visible request that navigation
// will approach. A disabled Serve at an old table does not justify cycling every
// carried dish before leaving that table.
func nextDinerTraySlot(v LiveView, goals []WorldFood, tried [5]bool) int {
	// Close diner bubbles can exceed the icon matcher scale range. At a native
	// disabled Serve, test each carried slot once; only green Serve permits input.
	if len(goals) == 0 {
		return nextTraySlot(v, tried)
	}
	best := -1
	for i, goal := range goals {
		if goal.Counter || v.Tray[goal.Dish] <= 0 {
			continue
		}
		if best < 0 || goal.Box[1]+goal.Box[3] > goals[best].Box[1]+goals[best].Box[3] {
			best = i
		}
	}
	if best < 0 || selectedTrayDish(v) == goals[best].Dish {
		return -1
	}
	for slot, dish := range v.TraySlots {
		if dish == goals[best].Dish && slot < len(tried) && !tried[slot] {
			return slot
		}
	}
	return -1
}
