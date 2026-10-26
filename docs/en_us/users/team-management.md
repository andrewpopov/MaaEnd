# Team management

Updated: 2026-10-20.

The **Team management** group connects saved build guides, account evidence, upgrade planning and material farming. Start with **Recommend teams**, choose a lineup in **Plan character upgrades**, then use **Farm team upgrade materials** after reviewing the costs. The preparation and combat-capture tasks also belong to this group.

## Recommend a team

Recommendations compare 42 saved lineups with the account roster. Fully owned teams appear first, followed by the recently inspected Mobalytics variants and operator/weapon level readiness. This is a guide-fit recommendation, not a simulated damage ranking or a claim that every guide requirement has been met. Follow each source for encounter assumptions, weapon Potential, attribute form, ULT Efficiency and rotation requirements.

A normalized account roster can be supplied from a historical review. Otherwise the current account-scan export is used as partial recommendation evidence. A newer normalized `data/TeamManagement/roster.json` can replace it using `account_uid`, `as_of` and `roster` fields. Missing or stale information remains visible. A gear snapshot shows the equipped plan only; it does not prove that alternate plans or owned items are missing.

## Use an existing gear plan

1. Open the operator, select **Gear**, then **Gear Plan** at the top right.
2. Select a named saved row. Check all four pieces, attributes and set effects in the preview. Selecting a row does not equip it.
3. Inspect the portraits on the gear. A plan can refer to pieces currently equipped by another operator. Check the entire team before moving shared items.
4. Choose **Apply** and inspect any sharing confirmation. Reopen **Gear** and verify the four equipped pieces and set effects.
5. Verify the resulting role and energy requirements. Arcane uses INT form when `INT >= WILL` and WILL form when `WILL > INT`. A plan name alone does not verify the form.

On 2026-10-20, the account had 15 saved plans. `HotWorksArcane` matched Arcane's equipped INT build at 707 INT and 335 WILL. The alternate WILL plan was reported by the user but was not verified during this inspection. Reuse and inspect saved plans before recommending new crafting.

## Save or update a gear plan

1. Equip the intended four pieces and verify their variants, attributes and set effects.
2. Open **Gear Plan** and select **Current Gear**. Enter a distinct role-based name in **Save As**, then choose **Save**.
3. Reopen the saved row and compare its four pieces and effects with the equipped build. Record physical item IDs when available, including any sharing with another operator.
4. To replace a saved plan, explicitly choose its overwrite control and verify the destination name first. Applying a plan and saving a plan are separate operations; do not overwrite or delete merely to equip it.

## Plan member upgrades

Choose the exact lineup and operator level, weapon level, normal skill rank (1–9) and separate Mastery (0–3) targets. Mastery 0 preserves current investment; it does not downgrade skills. The task creates a separate request under `data/TeamManagement/requests/` and writes JSON and HTML reports. Existing requests are preserved; changing their targets requires reviewing the cost sequence again. Member build records retain the guide's weapons, four-slot gear alternatives and skill investment priorities.

The upgrade queue supports operator levels, weapon levels, individual normal skills, separate Mastery steps, promotions, gear crafting and artificing costs. Mastery requires the normal skill at rank 9. Each costed step needs its operator, upgrade kind, before/after state, exact material costs, account UID, observation time and evidence files. Capture the current levels separately. Costs must cover the sequence from the current state to the target without overlap, gaps or overshooting. Unknown costs are not treated as free.

Use **Capture upgrade or gear-plan preview** on the visible game screen to save a fresh image and OCR under the management capture folder. Captures stay unreviewed until their quantities, item identities and account UID are checked.

The current implementation plans these upgrades. The separate member-level task provides a budget-limited operator level route for Elite 4 levels 80–90. The planner itself does **not** click promotion, weapon, skill, crafting, artificing or gear-plan controls. Those remaining execution routes still need screen recognition and result verification before automation can be enabled. After any manual upgrade, recapture levels and costs before using the remaining queue.

## Level a selected member

Open the selected team member’s **Overview**, choose its exact name and a target from 80 to 90 in **Level a team member (80–90)**, and set a T-Creds budget. A budget of 0 only previews the next level and saves its observed costs. A positive budget permits sequential level-ups up to that budget and target. Lower-level, promotion and weapon-level routes are not enabled by this task.

The route checks the account UID and team membership, recognizes the two Cognitive Carrier icons, reads selected quantities and currency, and rechecks the preview before each commit. It verifies the new level and exact carrier/currency deltas afterward. It saves a receipt before committing; an uncertain result leaves that receipt armed and prevents automatic retries. The 82 → 83 Perlica pilot was manually verified with 36,000 EXP and 13,043 T-Creds. The automated OCR/recognition route still needs an end-to-end preview validation after deployment.

## Farm missing materials

The farming task uses the selected team's verified costs and material reserves. It totals costs across the team, reports known stock and shortages, and sets each inventory target to `total upgrade cost + reserve`. It forces a fresh progression-material scan and delegates stage selection and reward tracking to the existing Supply Plan pipeline. It does not change the Full Daily Protocol Space target.

Sixteen existing Supply Plan resource groups are supported, including operator/weapon EXP, promotion and skill materials, and advanced materials such as Warp Plasma-Coated Slide. The report lists unsupported items separately; it does not guess a farming source. Unknown stock stays unknown in the report and is checked by the live inventory scan. Stale or mismatched cost evidence prevents farming.

Choose the farming formation and combat settings in this task. Medication refills default to **End task**; opting into medication uses the existing expiry controls. Existing Protocol Space success/failure controls remain available. The resource target check runs after rewards and stops farming that resource when its target is reached. Farming never automatically spends the resulting materials on upgrades.

## Implementation and validation

`tools/team_manager.py` supplies recommendation, plan and farm commands. `TeamManagementAction` invokes it, then applies a validated farming override; Pipeline owns the game navigation. Reports and requests are stored under `data/TeamManagement/`. Ten focused planner tests cover material totals and reserves, unknown inventory, stale or wrong-account costs, missing and overlapping upgrades, shared gear, Arcane's form, unsupported materials and roster matching.

The management tasks and farming handoff require a rebuilt Go service and refreshed resources. Live farming and upgrade execution have not been validated by this feature's offline tests. Keep the task disabled in daily presets until its live route has been checked.
