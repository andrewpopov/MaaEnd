# Innkeeping kitchen scheduling and delivery route plan

Astra reviewed the current implementation for this plan on October 20, 2026. The objective remains a verified three-star real run. Current Trial results do not meet it.

## Operating policy

Open the kitchen immediately after the session starts, before the first customer sits. Keep it open while ordinary orders appear in the top panel and useful batches are queued. Close shortly before the selected kitchen finishes the last preparation in its useful batch, allowing time to reach pickup. Collect food, deliver complete meals efficiently, return, and replenish the kitchens.

Target **two outstanding useful dishes per kitchen**, including active and queued preparations. Respect each kitchen's observed capacity of three. Two is a batching target, not permission to duplicate orders or cook four dishes when fewer are needed. Si continues handling drinks and payments.

Customer arrival time and waiting age do not determine priority. Optimize the food deliveries that complete meals and allow customers to finish eating and leave. Do not count serving a single portion as clearing a multi-dish order.

## Changes required in the existing code

The research driver in `tools/inn_fixed_trial.py` queues one Kitchen 1 dish, closes immediately, waits for it, and delivers one item. Its lowest-table-number selection and generic End Session return check cannot implement this policy.

The production code already has useful state and cooking timers, but four policies must change together:

- `reactive.go:PlanReactive` sorts demands by `WaitingSeconds` and serves the oldest compatible request. Replace this with meal completion planning.
- `transit.go:planTransitJourney` scores matching dish count divided by outbound travel cost. Include complete meals, the entire delivery sequence, return travel, and kitchen availability.
- `reactive_action.go:kitchenExitRequired` closes when any dish becomes due. Replace this with the selected batch departure deadline.
- The executor fallback closes unless `waitForImminentCook` sees a finish within two seconds. Replace that fallback too; changing the planner alone will not keep the kitchen open.

Reuse `cooking.go` for cook estimates and countdown reconciliation. Preserve the distinction between an estimated finish, an inspection due, and confirmed food on the counter.

## Customer and food accounting

Extend `liveSession`, `cookJob`, and `SeatView` instead of building another autonomous driver.

Give each customer a session, table, and visit generation. Give each requested dish copy a stable identity within that visit. Displayed slot indexes are insufficient because remaining icons can shift after service.

Track each physical copy through queue acknowledgement, confirmed active preparation, inspection due, verified counter stock, tray slot, and confirmed service. A copy can satisfy one request and occupy one state. Queue disappearance schedules inspection; it does not establish pickup or authorize a replacement cook. Unreadable or temporarily hidden requests remain unresolved.

Keep explicit reservations linking cook jobs and held food to requested copies. Reconcile them after observed customer changes. A diner actually leaving invalidates that visit's reservations; elapsed arrival age does not reprioritize it.

A service acknowledgement requires the selected tray copy to decrease and the same customer's matching request to become served, while preserving other requests. Use the protections in `fixed_routes.go:servedSeat`, strengthened with visit identity. Dining confirms meal completion. Eating, payment, and actual seat release remain separate states handled or observed alongside Si.

## Early opening and kitchen batching

Pipeline should transition directly from the verified active-session HUD to the visible Add Order control and a kitchen-open acknowledgement. Add Order is available from the active HUD; do not insert an unnecessary counter trip before opening it. Record kitchen-open time and first observed seating time to prove the early-opening requirement.

While the menu is open, use the kitchen-layout top-panel recognition in `perception.go` to collect ordinary orders. Gourmand requirements still need their dedicated inspection; an unparsed Gourmand does not authorize a guessed dish.

Inspect each kitchen's actual chef, specialties, available recipes, and displayed preparation times. Kitchen number alone does not establish a specialty. Cache these facts and invalidate timing estimates when the chef configuration changes.

Allocate up to two useful jobs per kitchen, acknowledging each insertion before choosing the next. Prefer combinations that complete customer meals across both kitchens. Do not automatically refill the first completed item while the second batch item is still cooking. Keep the third slot outside the baseline policy until measurements show a benefit.

## Preparation timers

First verify the queue mechanics in a bounded Trial: two distinct requested recipes in each kitchen, with observations at first completion and second activation. Establish whether work is FIFO or parallel within a kitchen, independent across kitchens, and progressing normally while the menu is open. Existing single-active-slot recognition suggests FIFO but is not sufficient proof.

Record input time, queue acknowledgement, confirmed active-start evidence, recipe duration, and screenshot acquisition interval separately. Anchor an active job to a confirmed start or reconciled active countdown. A queued job's predicted start remains an estimate until its activation is observed.

Store duration observations by recipe and chef configuration. Existing observations are Kitchen 1 crackers 14 seconds, fin dumpling 20, fries 25, soup 37; Kitchen 2 fries 23 and soup 34. Other combinations require observation.

Use finish windows rather than exact timestamps. Account for countdown rounding, capture delay, and measured variation. A timer authorizes checking the counter, never inventing stock or a slot. The native dish control plus tray increment confirms pickup.

## Joint kitchen and delivery scheduling

Enumerate a bounded set of candidate kitchen assignments, batches, pickup allocations, table sequences, and returns over approximately two delivery cycles. Six tables and two-job batches make this manageable.

Compare plans by:

1. More complete customer meals within the same horizon.
2. Earlier completion of those meals and earlier projected seat release where eating-time data exists.
3. Less travel, interaction overhead, and idle time.

When no meal can yet be completed, select preparation work that advances the earliest complete meal. Do not stall merely because the immediate completion count is zero. Arrival age is absent from the ranking.

A route's cost includes pickup, counter exit, middle-lane travel, table approaches, all service interactions, table exits, return, and the next pickup approach. Allocate copies across the whole trip so one food cannot satisfy two customers. Select carried slots explicitly when the desired dish is not in the default slot.

The normal close deadline is:

`selected useful batch finish - measured close-to-pickup lead - timing margin`

Choose the earliest useful kitchen batch tail, not the first individual dish's finish and not the latest finish across both kitchens. Do not keep extending the batch while completed meals sit waiting. Recompute at new orders, queue acknowledgements, timer boundaries, pickup, service, and waypoint arrivals.

An earlier trip is a candidate only if it completes a customer and its verified return fits before the remaining useful preparations. This makes early departure a planned tradeoff rather than an automatic reaction to any ready dish.

Use timers to schedule observation. Read the small order panel when orders can change, inspect countdowns near predicted transitions, and verify each interaction and route endpoint. Avoid repeated full-screen OCR and camera searches during known preparation windows.

## Physical route model

**Use the middle lane for every delivery and return.** Consistency is the current priority. The planner chooses dish bundles and table visit order; it does not choose between travel lanes. Do not add alternative paths or shortcuts in this phase. Revisit that optimization only after the middle-lane system is reliable.

Reuse directed routes, endpoint poses, and the loaded/empty evidence requirements in `productionTransit`. Establish physical waypoints for the counter hub, each row opening on the middle lane, each short table interaction approach, and every independently verified pickup position.

Each trip follows pickup, counter exit, middle lane, row opening, short table approach, service, return to the same middle-lane opening, middle lane, counter return, and pickup approach. Even consecutive table visits return to the middle lane between stops. Table approaches are interaction spurs, not alternative travel paths. Travel around tables and benches. Record reverse and left-side edges independently rather than inferring them from a forward or right-side route.

Camera azimuth can stay fixed while projection changes because the camera collides with the bar. Track physical pose and camera regime separately. A centered food marker or a large avatar does not prove interaction depth. Counter return must finish with physical endpoint evidence and a successful subsequent pickup, not merely End Session text.

Use `tools/inn_motion_lab.py` for offline landmark analysis, `inn_route_calibrate.py` for measured gestures, and `import_inn_transit.py` plus the existing route probe for promotion. Kitchen 1's observed right-to-left output order does not prove Kitchen 2's geometry. Queue capacity also does not establish the number of pickup positions.

Current evidence includes a 12.7-second Table 1 pickup/serve/return/next-pickup loop, a successful Table 5 delivery and return, and one complete 31-second cracker cycle. The following fries cycle needed an extra right 200 ms and a matching longer exit. Table 2's last test failed before pickup. The left 1050/right 900 counter pair and six backward corrections are not verified reusable geometry.

## Worked timing example

Assume the experiment confirms independent FIFO kitchens. Table 1 needs crackers and fin dumpling; Table 5 needs fries and soup.

Kitchen 1 starts crackers at confirmed time 0, followed by fin dumpling. Estimated finishes are 14 and 34 seconds. Kitchen 2 starts fries at time 3, followed by soup. Estimated finishes are 26 and 60 seconds.

Keep the kitchen open through the cracker's first completion because that portion alone does not complete Table 1. With an illustrative close-to-pickup lead of three seconds, leave near time 31. Verify both Kitchen 1 dishes around time 34, collect them, and complete Table 1.

The observed 12.7-second single-dish roundtrip is a baseline, not proof of this two-dish trip. Estimate return as time 46.7 plus the measured extra pickup, service, and uncertainty. Commit only when its upper bound fits the next useful batch deadline. Kitchen 2's batch tail is time 60; reach it near that deadline rather than holding the initial menu until both kitchens finish.

## Implementation and validation order

1. Add stable copy reservations, confirmed starts, timing windows, and useful batch identities. Test duplicate dishes, observation gaps, queue races, and customer turnover.
2. Replace age and table-number priority with completion planning. Test that a newer completable meal beats an older partial order, return costs matter, reservations cannot duplicate food, and each kitchen targets two jobs.
3. Integrate the early-open and batch-deadline policy into both planner and executor. Pipeline owns acknowledged UI transitions; Go computes plans and difficult recognition.
4. Establish settled pickup waypoints and independently verified middle-lane edges and short interaction spurs for both pickup areas and all six tables. Reject any planned journey that uses another travel lane. Require repeated loaded and empty roundtrips without accumulating drift.
5. Run a complete Trial with two-kitchen batching, correct reservations, consecutive successful pickups, completed meals, and no manual rescue.
6. Run the real session and archive the start, final score, and visible three-star result before Continue. Tie the mode and result to the same session. `result_evidence.go` currently records mode as unknown, so an ended screen or successful task is insufficient.

The immediate next experiment combines early kitchen opening with two distinct requested dishes per kitchen and narrow countdown observations. Validate FIFO and finish windows, then close for the first useful batch and verify every pickup. Isolate route closure at a settled pickup waypoint before repeating a full scoring attempt.

Follow repository formatting and targeted test requirements. The goal remains incomplete until the real-run evidence passes.


## Persistent Trial recording, 2026-10-21

The measured first-row loop now has an opt-in `trial_profile` integration on the existing reactive initializer. It requires an acknowledged native Trial selection in the same task, retains cooking jobs across navigation, and leaves pickup and Serve in the normal Pending acknowledgement flow. Ordinary automation still requires verified graph routes. The research profile does not grant production replay credit.

The profile file `data/Innkeeping/trial-routes.json` must contain reviewed geometry before activation. It is not generated from missing screenshots. Each primitive records actual before/after images on the configured image drive; a completed leg writes its gesture journal for importing candidate graph stages. The checkpointed sequence still requires a successful live replay.

The live recording also exposed a missing Welcome action for gourmands. Recognition already identified it; execution now validates and acknowledges Welcome independently of Serve. Unrecognized food requirements remain unparsed. The observed dessert/not-too-sweet plus no-sour combination now has coverage from actual saved-image MaaEnd OCR. Each two-line request is parsed separately; unfamiliar or extra constraints still reject the entire pair.

Evidence is under `I:/MaaEndData/Images/Innkeeping/canonical-first-row-20261021`. A successful input or a native pickup at a different counter slot does not establish canonical departure geometry. The continuous Trial profile and the three-star real run remain unverified.


Movement destination selection now shares the meal-completion ordering used by cooking. It considers only food carried on the current trip, then chooses an available verified route to the highest-priority reachable customer. Travel cost remains secondary; an unreachable preferred customer cannot block service to reachable customers. This policy has focused regression coverage; live validation is pending control handoff.
