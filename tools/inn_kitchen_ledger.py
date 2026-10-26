"""Cooking estimates for the live inn driver; OCR remains authoritative for stock."""
from collections import Counter


class KitchenLedger:
    def __init__(self, jobs, state=None):
        self.jobs = jobs
        self.kitchens = (state or {}).get("kitchens", {})
        self.refill_pending = set((state or {}).get("refill_pending", []))
        self.desyncs = (state or {}).get("desyncs", 0)
        self.tray = dict((state or {}).get("tray", {}))
        self.served = dict((state or {}).get("served", {}))

    def state(self, now):
        return {"kitchens": self.kitchens, "refill_pending": sorted(self.refill_pending),
                "desyncs": self.desyncs, "at": now, "tray": self.tray, "served": self.served,
                "counts": {phase: dict(Counter(j["dish"] for j in self.jobs
                    if self.phase(j, now) == phase)) for phase in
                    ("preparing", "awaiting_confirmation", "picked")}}

    @staticmethod
    def phase(job, now):
        if job.get("picked"):
            return "picked"
        if job.get("counter_check_due") or job.get("eta", 0) <= now:
            return "awaiting_confirmation"
        return "preparing"

    def reserved(self):
        return Counter(j["dish"] for j in self.jobs if not j.get("picked"))

    def cooking(self, kitchen, now):
        return [j for j in self.jobs if j["kitchen"] == kitchen
                and self.phase(j, now) == "preparing"]

    def order(self, now):
        # Refill the kitchen that will become idle first; avoid a fixed 2->1 tour.
        return sorted((1, 2), key=lambda k: (
            max((j.get("eta", now) for j in self.cooking(k, now)), default=now), k))

    def observe(self, kitchen, view, now):
        if (view.get("phase") != "kitchen" or view.get("kitchen") != str(kitchen)
                or not view.get("queue_verified") or view.get("unknown")):
            raise ValueError("Kitchen queue is not fully recognized")
        key = str(kitchen)
        cache = self.kitchens.setdefault(key, {})
        if view.get("recipes"):
            cache["recipes"] = view["recipes"]
        queue = Counter(view.get("queue") or {})
        if any(n < 0 for n in queue.values()) or sum(queue.values()) > 3:
            raise ValueError("Invalid observed kitchen capacity")
        cache.update(observed_at=now, queue=dict(queue), active=view.get("active_dish"))
        outstanding = [j for j in self.jobs if j["kitchen"] == kitchen and not j.get("picked")]
        remaining = queue.copy()
        selected = []
        # Latest copies still in the queue; older identical copies may be on the counter.
        for job in reversed(outstanding):
            if remaining[job["dish"]] > 0:
                selected.append(job)
                remaining[job["dish"]] -= 1
            else:
                job["counter_check_due"] = True
        mismatch = any(remaining.values())
        if mismatch:
            self.desyncs += 1
            for dish, count in remaining.items():
                for _ in range(count):
                    job = dict(dish=dish, kitchen=kitchen, picked=False, queued=now,
                               eta=now, adopted=True, counter_check_due=False)
                    self.jobs.append(job)
                    selected.append(job)
        selected.sort(key=lambda j: j.get("queued", 0))
        recipes = {r["dish"]: r for r in cache.get("recipes", [])}
        active = view.get("active_dish")
        seconds = view.get("active_seconds", 0)
        # A unique active copy can be reconciled even when adopted waiting order is unknown.
        active_job = next((j for j in selected if j["dish"] == active), None)
        duration = recipes.get(active, {}).get("seconds", 0)
        if active_job and 0 < seconds <= duration + 1:
            active_job.update(eta=now+seconds, counter_check_due=False)
            finish = active_job["eta"]
            if not any(j.get("adopted") for j in selected) and selected[0] is active_job:
                for job in selected[1:]:
                    duration = recipes.get(job["dish"], {}).get("seconds", 0)
                    if duration <= 0:
                        break
                    finish += duration
                    job.update(eta=finish, counter_check_due=False)
        # Unknown adopted timing calls for another observation; it never proves ready stock.
        cache["timing_uncertain"] = any(j.get("adopted") and j.get("eta", 0) <= now for j in selected)
        return not mismatch

    def plan(self, kitchen, view, demand, carried, now, initial=False, target=2):
        recipes = {r["dish"]: r for r in self.kitchens.get(str(kitchen), {}).get("recipes", [])
                   if r.get("available") and r.get("seconds", 0) > 0}
        queue = Counter(view.get("queue") or {})
        reserved = self.reserved()
        need = Counter(demand)
        if initial:
            need |= Counter({d: 1 for d in recipes})
        chosen = []
        while sum(queue.values()) < min(3, target):
            candidates = [d for d in recipes if need[d] > reserved[d]+carried[d]]
            if not candidates:
                break
            # Measured chef-adjusted times determine specialties, never kitchen number.
            dish = max(candidates, key=lambda d: (
                (need[d]-reserved[d]-carried[d])/recipes[d]["seconds"],
                -recipes[d]["seconds"], d))
            chosen.append(recipes[dish])
            reserved[dish] += 1
            queue[dish] += 1
        return chosen

    def queued(self, kitchen, recipes, issued_at):
        finish = max((j.get("eta", issued_at) for j in self.jobs
                      if j["kitchen"] == kitchen and not j.get("picked")
                      and not j.get("counter_check_due")), default=issued_at)
        finish = max(finish, issued_at)
        for recipe in recipes:
            finish += recipe["seconds"]
            self.jobs.append(dict(dish=recipe["dish"], kitchen=kitchen, picked=False,
                queued=issued_at, eta=finish, counter_check_due=False))

    def refill_due(self, demand, carried, now):
        if not any(n > self.reserved()[d]+carried[d] for d, n in demand.items()):
            return False
        return bool(self.refill_pending) or any(len(self.cooking(k, now)) < 2 for k in (1, 2))

    def invalidate_timers(self):
        # Blaze changes speed; retain reservations but request counter/timer confirmation.
        for job in self.jobs:
            if not job.get("picked"):
                job["counter_check_due"] = True
        self.refill_pending.update((1, 2))
