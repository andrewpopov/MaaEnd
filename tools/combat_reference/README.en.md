# Combat reference library

Retrieved on 2026-10-17 (America/Los_Angeles). This local library contains 34 distinct team compositions and 18 published rotation examples from 13 Prydwen character guides. It includes Heat, Cryo, Electric, Physical, and Nature teams. The original guides remain the authority for build assumptions and updates.

## Files

- `catalog.json`: team members, source links, ordered action examples, prerequisites, and four timeline examples containing 168 events.
- `roster_resume.json`: the deferred roster scan checkpoint and emulator resume requirements.
- `../Capture-Roster.ps1` and `../capture_roster.py`: the capture launcher and implementation.

## Rotation coverage

Twelve compositions have explicit rotation examples. Other compositions are saved for comparison with their source links; they do not inherit a different lineup's sequence. Openers and setup fragments are labeled separately from scenario sequences. None has been validated against this account's finished roster, equipment, or live combat.

The published examples include full-energy bursts, zero-energy starts, Wulfgard P5 routing, Crush/Breach alternatives, and timelines for Arcane, Mi Fu, Typhoeus, and Zhuang Fangyi. Some guides list alternatives without complete rotations. The inspected Si guide omitted the fourth portrait from its team blocks, so it is recorded in the review queue instead of filling that slot by assumption.

## Timing and execution

Actions retain operator names. A null operator means the source leaves the controlled operator unspecified. Repeated final strikes represent completed basic-attack strings, not single attack taps. Combo availability, SP, infliction stacks, target state, operator changes, Potential levels, and gear effects must be respected. Rossi's second combo input needs its own timing check.

Timeline seconds come from the displayed diagram axis and event positions. They are schematic source positions, not measured input timings. Events at the same position have no verified serial input order. These files are reference data, not executable EndAxis codes or enabled MaaEnd combat profiles. The existing importer only covers skills and ultimates; the remaining actions need implementation and live validation.

## Deferred scan

The earlier PC archive contains six operators and 48 screenshots in the capture directory recorded by `roster_resume.json`. It remains partial historical evidence. The automatic UI tasks use the verified Endies emulator's English 1280x720 layout and bind their separate runtime session to the observed UID. Keep Arkies/Arknights untouched.

Use the four core account scan tasks in MaaEnd to collect current operator, ability, weapon and gear evidence. `../Capture-Roster.ps1` remains the earlier capture helper and does not drive these new batch routes. Review the resulting account data before selecting teams or spending crafting and upgrade materials.

## All-character builds

`characters.json` and `characters/` contain the build records for all 34 directory entries: 33 published builds and one upcoming character, Argent Flow. The records preserve 99 weapon recommendations with their weapon Potentials, 54 complete four-slot gear loadouts, and 36 role-specific skill investment orders. `guides/INDEX.txt` links the individual original summaries and source guides. Team and rotation IDs link back to `catalog.json`.

This snapshot keeps the first three weapon recommendations per general or specialist section and the first two complete gear loadouts per guide. Additional alternatives, precise stat thresholds, calculations, and detailed combat explanations remain available in the source guides. No independent stat ranking is inferred, and none of these builds has been applied to the account.

## Account scan tasks and storage

`account_scan_tasks.json` defines twelve read-only tasks: account identity, operators, abilities/talents, equipped weapons/essences, weapon inventory, equipped gear, gear inventory, saved loadouts, crafting recipes, crafting materials/currencies, artificing, and team presets. Each task lists its required fields, evidence views, dependencies, and navigation readiness. `account_scan_resume.json` points to the current session and is linked from Si's plan.

The store is `install/data/AccountScans/account_scans.sqlite3`, with original screenshots under `screenshots/` and portable session snapshots under `exports/`. Account labels are separate from verified UIDs. Capture times are preserved when importing the earlier roster archive. That archive supplies 48 images as partial evidence, with zero parsed or reviewed account records.

Run these commands from the repository root, using the session ID in `account_scan_resume.json`:

```powershell
.\tools\Scan-Account.ps1 status --session <session-id>
.\tools\Scan-Account.ps1 export --session <session-id>
.\tools\Scan-Account.ps1 capture --session <session-id> --task gear_recipes --entity recipe-name --page cost-preview --instance <instance-id>
.\tools\Scan-Account.ps1 import-image --session <session-id> --task gear_recipes --entity recipe-name --page cost-preview --input screenshot.png
```

`capture` only saves the already-open page through MaaEnd's screenshot subscription. Verify its content and freshness during review. It does not navigate to recipes, craft, equip, upgrade, or artifice. Automatic navigation for unmapped screens remains pending until verified on Endies; the earlier roster navigation is also not yet validated on the emulator.

Use `record --input record.json --evidence <evidence-id> ...` to attach parsed JSON to an entity. Add `--reviewed` only after checking every required field against the linked screenshots. Unknown quantities remain null, and an inventory preview must not be assumed equipped. Evidence cannot cross accounts, tasks, or entities. Completion uses `complete --coverage-note "..."`; it requires reviewed dependencies, reviewed records, and coverage of every captured page. New evidence invalidates the relevant reviews and downstream task completion. Records and exports are local; no account data is uploaded.

## Craft, equip and upgrade paths

`../gear_workflow.py` builds an offline preview from the account store. `plans/si-gear-request.json` selects Si's four proposed pieces; `install/data/GearPlans/si-preview.json` contains the current missing-data report. The planner does not send game inputs. Crafting, equipment verification, saving/reopening a loadout and single artificing attempts have step contracts; emulator navigation, weapon leveling/promotion and skill upgrades remain pending.

```powershell
.\.venv\Scripts\python.exe tools/gear_workflow.py --session 7ea55097fa8a4daa80c52100629d39f5 --request tools/combat_reference/plans/si-gear-request.json --output install/data/GearPlans/si-preview.json
```

Requests contain `operator`, `loadout_name`, four `gear` slots (`armor`, `gloves`, `kit_1`, `kit_2`), `spend_limits`, `donor_allowlist` and `artifice_attempts`. Each gear target has `item_name` and optional exact `item_id` or `recipe_id`. A conflicting saved name requires its explicit `overwrite_plan_id`. Owned copies are reused before crafting; ambiguous copies require selection. Every physical inventory copy must have a unique `item_id` and `quantity: 1`; inventory kit records use `slot: "kit"`. Items equipped by another operator are excluded. Recipe batch surplus is reserved before planning another craft.

Recipe `materials` and `currency_cost` must each be explicit resource-name-to-integer objects, including `{}` for an observed zero cost. `batch_size`, `recipe_id`, `technique`, slot and unlock state must be known. Material/currency balances must have unique names and known quantities. Shared totals across crafting and artificing must fit both balances and explicit `spend_limits`; missing costs or limits block the preview.

Each artificing request names `target_item_id`, `donor_item_id` and `stat_line`. The matching reviewed `artificing` record supplies current/maximum levels, pity, match quality, `success_chance` as a fraction from 0 to 1, and explicit `catalyst_cost`/`other_costs` objects. The donor must be explicitly allowed, unequipped, unlocked, unfavorited, unenhanced and slot-compatible. Final equipment cannot become a donor. Only one attempt per target is planned; its result and actual consumption must be rescanned before another attempt. No fixed number of attempts guarantees a stat level.

Account identity and relevant scan coverage must be reviewed, linked records must be at most 24 hours old, and stored evidence checksums must match. Every preview includes the input fingerprint and keeps `execution_enabled: false`. A future executor must verify the active account, live preconditions, costs and item IDs before each action, bind newly crafted copies from observed inventory changes, and stop on an uncertain result instead of repeating a possible spend. Si's current preview is blocked because the screenshots have not yet been reviewed into account records.

## Manual workflow recordings

`../record_workflow.py` records the already-connected native controller through MaaEnd's local screenshot API. Pass `--api-url http://127.0.0.1:<port>/api`, `--instance <instance-id>` and a new `--output <directory>`. It sends only screenshot subscribe/unsubscribe requests and screenshot reads; it does not capture clicks, keys or audio and does not start game tasks. Original PNGs are deduplicated by SHA-256, while `frames.jsonl` preserves every sample timestamp. OCR processing remains a separate step.

The recorder renews its subscription every 10 seconds because MXU expires subscribers after 30 seconds. `status.json` includes the last change time and flags frames unchanged for 15 seconds; an unchanged image can also mean a stationary scene. Create `STOP` in the output directory to finish. Recording also stops after 30 minutes, 1 GiB of unique PNGs or three consecutive capture errors. The first inn recording lacked renewal, so frames after its first 30 seconds cannot establish the later workflow.

## Offline OCR review

`../review_workflow_ocr.py <recording-directory>` processes every unique archived PNG with the installed MaaEnd OCR models. It binds resources only, with no live controller. The resumable `ocr/results.jsonl` preserves complete OCR results, bounding boxes and confidence scores; `ocr/transcript.txt` and `ocr/timeline.json` include text scoring at least 0.8 and omit UID lines. Raw OCR remains local and may include identifiers. `ocr/summary.json` records coverage and model hashes. Run `../review_inn_signals.py <recording-directory>` afterward to generate `ocr/inn-review.json` with timestamped evidence for inn states, queue warnings, customer exclusions, helper bill collection and session completion. Optional `ocr/visual-review.json` annotations remain distinct from OCR observations.

Screenshot times are sampling times, not input events. OCR does not identify unnamed dish icons, reconstruct player inputs, prove navigation paths or establish an optimal strategy. Review important results against the original PNGs, exclude stale initial cache images and transitions, and use template/icon recognition alongside OCR for future inn automation.

## Page readers and inn action status

The four core account tasks now have automatic Endies/ADB routes: owned operator Overviews, all four Ability Matrix skill details and the visible talent nodes, equipped weapons/essences/Potential, and the four equipped gear slots. They reset the roster to its top, visit each operator, verify page headers, and save named screenshots with OCR text and bounding boxes. Roster and equipment-detail scrolling are bounded and must establish a stationary end. Individual matrix nodes are aligned from visible glyphs; Endministrator uses its observed eight-node tree instead of the regular twelve-node tree. Detail captures wait for their actual category heading. Native controllers retain the current-page reader. Other inventory, recipe, material and team tasks still require their page to be open.

The batch uses the observed UID to keep accounts separate. `ui_session.json` points to the SQLite session and readable `exports/<session-id>-ocr.txt`; JSON exports preserve page-level evidence and draft fields. Capture status `captured` requires every expected page for every visited operator and checks detail-run names against the recent owned-roster capture, while missing or unreadable pages fail with saved partial results. Captured data remains unreviewed: it neither verifies inventory nor enables crafting, upgrading or equipment changes. The owned-roster route passed a live 29-operator traversal on 2026-10-18. Ability and equipment routes remain under full-roster validation; consult the task specification for current navigation readiness. The remaining page readers are marked [Manual page] in the UI.

`Innkeeping: read next action` is a bounded, read-only UI action. It classifies the recorded English inn states and writes one report under `data/Innkeeping`; it neither starts a round nor cooks, serves or moves. The separate planner prioritizes held compatible food, demanded ready food, uncovered confirmed cooking demand and uncollected bills. It reserves queued output once, checks observed chef-specific cooking time against travel time and the remaining window, respects full queues and helper collection, and avoids speculative cooking. Dish requirements and stock must be verified inputs; OCR-only live reads explicitly report the missing icon mapping instead of guessing. Automated dish selection, movement and input/result verification remain unimplemented. Replay tests cover all 564 recorded frames; these are recognition checks, not a live autoplay validation.
