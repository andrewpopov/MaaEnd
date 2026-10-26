package innkeeping

import (
	"sort"
	"time"
)

// reconcileBatchCounts retains the issued count until every copy in that
// kitchen's batch has been picked up, including counter candidates.
func (s *liveSession) reconcileBatchCounts() {
	if s.BatchIssued == nil {
		s.BatchIssued = map[string]int{}
	}
	counts := map[string]int{}
	for _, job := range s.Jobs {
		counts[job.Kitchen]++
	}
	for id := range s.BatchIssued {
		if counts[id] == 0 {
			delete(s.BatchIssued, id)
		}
	}
	for id, n := range counts {
		s.BatchIssued[id] = max(s.BatchIssued[id], n)
	}
}

// holdForBatch schedules observation, never pickup. The initial three-second
// departure allowance is provisional until close-to-pickup timing is calibrated.
// Unknown preparation times cannot establish a batch departure deadline.
func holdForBatch(jobs []cookJob, v LiveView, now time.Time) bool {
	if v.Phase != "kitchen" || trayCount(v.Tray) > 0 {
		return false
	}
	tails := map[string]time.Time{}
	useful := 0
	for _, job := range jobs {
		if !neededPickup(v, job.Dish) {
			continue
		}
		useful++
		finish := job.ExpectedAt
		if job.CounterCheckDue || job.Phase == "ready" {
			finish = now
		}
		if finish.IsZero() {
			return false
		}
		if finish.After(tails[job.Kitchen]) {
			tails[job.Kitchen] = finish
		}
	}
	if useful == 0 {
		return len(jobs) == 0 && len(v.Seats) > 0 && !hasFoodDemand(v)
	}
	var earliest time.Time
	for _, tail := range tails {
		if earliest.IsZero() || tail.Before(earliest) {
			earliest = tail
		}
	}
	return now.Before(earliest.Add(-3 * time.Second))
}

func hasFoodDemand(v LiveView) bool {
	for _, seat := range v.Seats {
		if len(seat.Dishes) > 0 || seat.Phase == "gourmand" {
			return true
		}
	}
	return false
}

// completionDemands groups a customer's remaining dishes and prefers meals
// completable from physical stock, then meals with fewer missing preparations.
// Arrival age is telemetry only. Each physical copy is allocated at most once.
func completionDemands(s ReactiveState) []Demand {
	groups := map[string][]Demand{}
	for _, d := range s.Demands {
		groups[d.Seat] = append(groups[d.Seat], d)
	}
	stock := map[string]int{}
	for id, n := range s.Tray {
		stock[id] += n
	}
	for id, n := range s.Ready {
		stock[id] += n
	}
	var ordered []Demand
	for len(groups) > 0 {
		bestSeat := ""
		bestMissing, bestTotal := int(^uint(0)>>1), int(^uint(0)>>1)
		var bestStock map[string]int
		for seat, demands := range groups {
			remaining := map[string]int{}
			for id, n := range stock {
				remaining[id] = n
			}
			missing := 0
			for _, d := range demands {
				covered := false
				for _, id := range d.Allowed {
					compatible := !d.Gourmand
					if d.Gourmand {
						for _, recipe := range s.Dishes {
							if recipe.ID == id && d.Requirements.Accepts(recipe) {
								compatible = true
								break
							}
						}
					}
					if compatible && remaining[id] > 0 {
						remaining[id]--
						covered = true
						break
					}
				}
				if !covered {
					missing++
				}
			}
			if missing < bestMissing || (missing == bestMissing && (len(demands) < bestTotal || (len(demands) == bestTotal && (bestSeat == "" || seat < bestSeat)))) {
				bestSeat, bestMissing, bestTotal, bestStock = seat, missing, len(demands), remaining
			}
		}
		demands := groups[bestSeat]
		sort.SliceStable(demands, func(i, j int) bool { return demands[i].ID < demands[j].ID })
		ordered = append(ordered, demands...)
		stock = bestStock
		delete(groups, bestSeat)
	}
	return ordered
}
