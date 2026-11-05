# Design: Comparison-B transfer matcher (approved)

Experiment matcher under `experiments/transfermatcher`. Core stays a pure simulator; transfer **selection** never enters core. Builds on approved handoff primitives (PR #24 / `d6be452`). Approved by Technical Program Manager for implementation of the matcher PR only; **no Comparison-B sweep runs until that PR is reviewed.**

**Caveat (TPM):** handoff safety depends on deterministic plug-in behavior and a configured `max_handoff_wait_seconds`. This is a **transfer-enabled service policy vs established greedy**, not a uniform-detour-policy test and **not** proof that transfers help. Comparison A is unchanged.

## Comparison and fixed settings

**Arms (pre-registered B).** Control = Comparison-A **built-in** `greedy_nearest_eta` with pooling on (unlimited pooling detours). Treatment = the **same** greedy ordinary behavior, plus an optional transfer only when a candidate passes the path ratio below. Ordinary non-transfer assignments must be greedy-identical (strictly earlier pickup to accept a transfer).

| Setting | Control | Treatment |
|---|---|---|
| `[hooks] matching` | `greedy_nearest_eta` | `transfermatcher` (greedy-identical when no transfer wins) |
| `pooling_enabled` / `reassignment_enabled` | true / true | true / true |
| `max_candidate_cars` | 10 | 10 |
| `handoffs_enabled` | false | **true** |
| `transfers_enabled` | (n/a) | **true** (experiment-only; false ⇒ greedy ordinary + onward rematch only) |
| `max_handoff_wait_seconds` | (off) | **300** |
| `max_request_to_assignment_seconds` | **300** | **300** |
| `max_request_to_pickup_seconds` | **1200** | **1200** |
| `max_rider_path_ratio` | (n/a) | **1.25** |
| `max_insertion_evaluations` | (n/a) | **5000** (transfer/provisional/onward scoring only) |
| Fleets · build/supply seeds | 70%, 100% · 1/2/3 · 20161001/2/3 | same |
| Router | `straight_line` @ 8.0 m/s | same |

## Transfer path ratio (treatment only)

Constant **`max_rider_path_ratio = 1.25`** (no extra absolute tolerance), applied only when scoring a **transfer** candidate — never to ordinary greedy insertions.

- **OD distance** for the cap uses the same router Estimate/Route single-leg distance as path sums (not a separate haversine when the router differs).
- **At assignment (provisional):** Estimate aboard path O→…→H on A’s post-insertion pooled plan plus Estimate H→…→D on the paired provisional B plan; accept only if sum ≤ 1.25 × dist(O,D) and H ≠ O,D. Upper-bound filter only (see rematch).
- **At every live `waiting_for_handoff` match / reassignment:** for each currently feasible B (deterministic rank), require **realized first-leg distance** + Estimate of pooled H→…→D on B’s candidate plan ≤ 1.25 × OD. Same full cap on later onward reassignments.

### Realized first-leg distance

`RequestView.RealizedAboardDistanceMeters` is the sum of completed trip-leg distances (after the first handoff dropoff = exact O→…→H driven). Read-only; no simulation decisions in core.

## `transfers_enabled`

Experiment-only `[matching] transfers_enabled`, recorded in run metadata when set. Default **true** when unset for `transfermatcher`. When **false** (even with `handoffs_enabled=true`): ordinary requests use built-in greedy behavior (byte-identical plans/outputs to greedy on the same handoffs-on schema); `waiting_for_handoff` riders are still rematched onward.

## At most one transfer per rider

`requested` / reassignable `assigned`: greedy single-car **or** one provisional transfer. `waiting_for_handoff`: onward at exact H → final D only. Never a second handoff dropoff.

## Transfer-point candidates (deterministic, per A/B pair)

1. First-leg cars A: ≤ N open + N busy nearest O, `N = max_candidate_cars`.
2. Provisional onward cars B: ≤ N open + N busy nearest D, excluding A.
3. **For each (A, B) pair** in stable `supply_id` order: candidate H points = geodesic fractions 1/3, 1/2, 2/3 on O→D **plus that B’s current location**, deduped, excluding O/D (up to four points per pair).
4. Rank surviving (A, H) by pickup ETA at O; ties `A.supply_id`, then H lat/lng. Provisional B for a pair is that same B (existence filter only — identity is not preferred at rematch).

## Matcher architecture (no duplicate greedy)

Ordinary (non-handoff) requests are **always** matched by delegating to the built-in `matching.NewGreedy()` implementation. There is no second greedy to drift. Transfer search and onward rematch are layered on top of the greedy result:

1. Split requests into ordinary vs onward (`waiting_for_handoff` / already holding `HandoffLocation`).
2. **Freeze (v1 handoff path-cap policy):** any supply whose **current working plan** already contains a **handoff dropoff** or an **onward pickup** is unavailable for (a) ordinary greedy insertions (marked `going_offline` for the greedy input only) and (b) new first-leg A, provisional-B, and different-onward-B candidates in transfer/onward search. The currently assigned onward car may keep its plan; moving **that same** rider to another eligible (unfrozen) car is fine. **Availability cost:** cars busy with any transfer leg are excluded from pooling new handoff-related work until that work clears (pickup, cancel, matcher removal, or completed handoff) — conservative so earlier riders' capped paths cannot be lengthened by later overlays.
3. Run built-in greedy on ordinary requests (handoff riders excluded). `transfers_enabled=false` still takes this path even when an unrelated waiting rider exists — there is no early bypass that skips onward rematch or ordinary greedy.
4. If `transfers_enabled`, overlay transfers: for each ordinary request in `requested_at` / `request_id` order, search (A, B, H); accept only if transfer pickup ETA is **strictly earlier** than the greedy assignment’s pickup ETA; strip **only that request** from its greedy car (if any) and install only A’s `pickup(R @ O) … dropoff(R @ H)`. Candidate A/onward plans that would drop any other rider’s existing pickup are skipped (ownership must not depend on core `validChange`, which allows dropping merely-assigned pickups when reassignment is on). All other greedy plan changes are preserved.
5. Rematch all onward / waiting riders under the realized path-ratio cap.

## Provisional selection (matches core v1)

**Commit only A** at selection. Onward is matched after the actual handoff dropoff.

Per open request (`requested_at`, then `request_id`):

1. Built-in greedy best single-car plan (delegated; min total duration).
2. If `transfers_enabled` and transfer budget remains, search transfer (A, B, H) under the provisional ratio. For each (A, H), score **every** A insertion and keep the earliest-pickup plan whose path O→…→H leaves a nonnegative remaining budget **and** has a feasible paired-B onward within the total cap — do not pick an ETA-best over-cap plan then reject the pair.
3. Accept transfer only if pickup ETA is **strictly earlier** than greedy; return **only** A’s `PlanChange`: `pickup(R @ O) … dropoff(R @ H)`.
4. **Rematch:** full re-evaluation over currently feasible B cars. Prefer cars that are not empty-at-H (core may still be dwelling after `finishStop` and reject new plans for that car); fall back to empty-at-H only when no other candidate exists. Apply realized-distance + H→…→D cap. If none before 300s → `max_handoff_wait`.

## Complexity bound and evaluation budget

**One insertion evaluation** = one complete candidate scoring operation used by transfer / provisional / onward search: a single Route/Estimate of the candidate plan **including** the aboard path-distance derived from those same legs.

- Ordinary matching: **uncapped built-in greedy** (the established baseline). The experiment budget must not silently degrade greedy.
- Transfer / provisional / onward search: skip a supply whose current plan has more than **`P_max = 8`** stops (`I ≤ 45` when enumerated).
- **Hard ceiling `max_insertion_evaluations = 5000`:** scoped to transfer / provisional-B / onward candidate scoring only. Built-in greedy router calls are outside this ceiling. OD single-leg distance used for the path-ratio cap is also outside it. This is **not** a total greedy+transfer router-call claim; if a total bound is ever needed, add a separate wrapper budget rather than overloading this setting.

## Small smoke fixtures

| # | Scenario | Assert |
|---|---|---|
| 1 | Transfer ETA wins (ordinary straight-line) | Only A committed pre-dropoff; after dropoff, rematch assigns B; `handoff_count=1`, two `trip_legs`; realized+second ≤ floor(1.25×OD) |
| 2 | Rematch path ratio | realized + pooled H→…→D > 1.25×OD → that B rejected |
| 3 | Before handoff dropoff | Second-leg pickup at/after handoff dropoff (same-car handoff allowed) |
| 4 | `transfers_enabled=false` | Plans/outputs byte-identical to built-in greedy on handoffs-on pooling+reassignment fixture |
| 5 | Onward reassignment / ratio | Realized first-leg enforced on rematch |
| 6 | Same inputs twice | Byte-identical trips / legs / events |
| — | Evaluation budget | Tiny transfer budget still lets built-in greedy assign; transfer search stops; no total-call claim |
| — | Realized view | Capturing matcher: 0 pre-dropoff; equals completed-leg sum at live handoff |
| — | Compliant insertion | ETA-best over-cap insertion loses to a later compliant insertion |
| — | Handoff-leg freeze (v1) | Ordinary greedy, new transfer A/B, and different-onward B cannot insert onto a car that already holds a handoff dropoff or onward pickup; existing transfer leg path unchanged |
| — | Busy transfers_off | Many ordinary requests + unrelated waiting rider: ordinary plans == greedy; onward rematched |

## Out of scope for the matcher PR

Comparison-B sweep runs; altering Comparison A; OSRM; new seeds/fleets; walking; public materials; proof of benefit.
