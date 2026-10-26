package innkeeping

import "time"

// nextCookCompletion compares both kitchens without rereading their menus.
// Counter candidates and physical stock are handled by pickup inspection.
func nextCookCompletion(jobs []cookJob, v LiveView) *cookJob {
	var next *cookJob
	for _, job := range jobs {
		if job.CounterCheckDue || job.Phase == "ready" || job.ExpectedAt.IsZero() || !neededPickup(v, job.Dish) {
			continue
		}
		if next == nil || job.ExpectedAt.Before(next.ExpectedAt) {
			copy := job
			next = &copy
		}
	}
	return next
}

// estimatedCook records input time separately from acknowledgement time. These
// estimates schedule inspection; they never establish physical counter stock.
func estimatedCook(jobs []cookJob, p pendingInput, kitchen ChefKitchen, onlyActive bool, acknowledgedAt time.Time) cookJob {
	queuedAt := p.IssuedAt
	if queuedAt.IsZero() {
		queuedAt = p.At
	}
	if queuedAt.IsZero() {
		queuedAt = acknowledgedAt
	}
	seconds := p.CookSeconds
	if seconds <= 0 {
		seconds = kitchen.CookSeconds[p.Dish]
	}
	job := cookJob{Dish: p.Dish, Kitchen: p.Kitchen, QueuedAt: queuedAt, CookSeconds: seconds}
	if seconds <= 0 {
		return job
	}
	start := queuedAt
	if !onlyActive {
		for _, prior := range jobs {
			if prior.Kitchen != p.Kitchen || prior.Phase == "ready" || prior.CounterCheckDue {
				continue
			}
			if prior.ExpectedAt.IsZero() {
				return job // Unknown earlier work cannot supply a queue ETA.
			}
			if prior.ExpectedAt.After(start) {
				start = prior.ExpectedAt
			}
		}
	}
	job.StartEstimate = start
	job.ExpectedAt = start.Add(time.Duration(seconds * float64(time.Second)))
	return job
}

// reconcileCookCountdown associates the visible queue with its remaining FIFO
// copies, not older same-dish food waiting on the counter. Only known insertion
// order permits propagation beyond the observed active copy.
func reconcileCookCountdown(jobs []cookJob, v LiveView, kitchen ChefKitchen, capturedAt time.Time) {
	if v.Phase != "kitchen" || !v.QueueVerified || capturedAt.IsZero() {
		return
	}
	remaining := map[string]int{}
	for dish, count := range v.Queue {
		if count < 0 {
			return
		}
		remaining[dish] = count
	}
	selected := map[int]bool{}
	for i := len(jobs) - 1; i >= 0; i-- {
		j := jobs[i]
		if j.Kitchen == v.Kitchen && j.Phase != "ready" && remaining[j.Dish] > 0 {
			selected[i] = true
			remaining[j.Dish]--
		}
	}
	for _, count := range remaining {
		if count != 0 {
			return
		}
	}
	for i := range jobs {
		if jobs[i].Kitchen == v.Kitchen && jobs[i].Phase != "ready" {
			// Absence from a verified cooking queue schedules inspection only.
			// It does not establish the dish's physical counter slot or stock.
			jobs[i].CounterCheckDue = !selected[i]
		}
	}
	if v.ActiveDish == "" || v.ActiveSeconds <= 0 {
		return
	}
	var order []int
	knownOrder := true
	for i := range jobs {
		if selected[i] {
			order = append(order, i)
			knownOrder = knownOrder && !jobs[i].QueuedAt.IsZero()
		}
	}
	if len(order) == 0 {
		return
	}
	active := order[0]
	if !knownOrder {
		// An adopted queue's map counts do not reveal its waiting order.
		// A unique active dish can still supply its own countdown estimate.
		if v.Queue[v.ActiveDish] != 1 {
			return
		}
		for _, i := range order {
			if jobs[i].Dish == v.ActiveDish {
				active = i
				break
			}
		}
	}
	if jobs[active].Dish != v.ActiveDish {
		return
	}
	seconds := jobs[active].CookSeconds
	if seconds <= 0 {
		seconds = kitchen.CookSeconds[v.ActiveDish]
	}
	if seconds <= 0 || v.ActiveSeconds > seconds+1 {
		return // Reject merged OCR such as 311s for an 11-second recipe.
	}
	finish := capturedAt.Add(time.Duration(v.ActiveSeconds * float64(time.Second)))
	jobs[active].CookSeconds = seconds
	jobs[active].ExpectedAt = finish
	jobs[active].StartEstimate = finish.Add(-time.Duration(seconds * float64(time.Second)))
	jobs[active].Phase = "cooking"
	if !knownOrder {
		return
	}
	for _, i := range order[1:] {
		seconds := jobs[i].CookSeconds
		if seconds <= 0 {
			seconds = kitchen.CookSeconds[jobs[i].Dish]
		}
		if seconds <= 0 {
			return
		}
		jobs[i].StartEstimate = finish
		finish = finish.Add(time.Duration(seconds * float64(time.Second)))
		jobs[i].CookSeconds, jobs[i].ExpectedAt = seconds, finish
	}
}

// A retained job is scheduling state, not a fresh cooking observation. In
// particular, walking to its counter later cannot prove it cooked until then.
func directlyObservedCook(job cookJob, v LiveView) bool {
	if job.Phase == "ready" {
		return v.Prompt == "pickup" && v.PromptDish == job.Dish
	}
	if v.Phase != "kitchen" || !v.QueueVerified || v.Kitchen != job.Kitchen || v.Queue[job.Dish] <= 0 {
		return false
	}
	if job.Phase == "queued" {
		return true
	}
	return job.Phase == "cooking" && !job.CounterCheckDue && v.ActiveDish == job.Dish && v.ActiveSeconds > 0
}
