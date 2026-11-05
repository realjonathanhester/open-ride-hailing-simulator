# Design: generic core handoff primitives

Approved design (with TPM implementation corrections applied). Transfer selection stays under `experiments/`. Core stays a pure simulator; companies plug in matching, routing and experiment logic through existing hooks. Transfer **selection and matching** stay under `experiments/`, never in core.

## Problem

Today a plan stop is `{request_id, type: pickup|dropoff}` and the simulator resolves its location from the request origin or destination (`simulator.replan`). A rider who boards stays with that car until destination (`on_trip` is never reassigned; aboard riders keep their dropoff). There is no way to express an intermediate vehicle change, a stop that is not origin/destination, or a rider waiting between legs. Companies that need multi-leg trips need small, generic primitives—not a pooling-specific feature.

## Located stops and v1 cap

Extend the matcher's stop with an optional explicit location:

| Field | Meaning |
|---|---|
| `request_id`, `type` | Unchanged (`pickup` or `dropoff`) |
| `location` | Optional `{lat, lng}`. When unset, behavior is byte-identical to today: pickup → origin, dropoff → destination |

**Rules.** A dropoff at the request destination (location absent or equal) is always **final** and completes the trip. A **handoff dropoff** must use an explicit location that **differs** from the destination; the rider leaves the car and is not completed. A **handoff pickup** (pickup of a `waiting_for_handoff` rider) must use exactly that rider's current handoff location; any other location is rejected. Matchers that never set `location` are unaffected.

**v1 cap.** The core allows at most one outstanding onward pickup per rider at a time. The experiment enforces one transfer total per rider for Comparison B.

## Rider state and lifecycle

Add status `waiting_for_handoff`:

```
requested → assigned → driver_arrived → on_trip → completed
     ↘ canceled          ↘ canceled       │
assigned ⇄ requested (reassignment)       ├→ waiting_for_handoff → assigned → …
                                          └→ canceled
waiting_for_handoff → canceled
```

- **Handoff dropoff:** `on_trip` → `waiting_for_handoff`; rider leaves `aboard`, seats free; open to the matcher as waiting (not `requested`).
- **Handoff pickup:** `waiting_for_handoff` → `assigned` → `driver_arrived` → `on_trip`, same dwells as a first pickup.
- **Final dropoff:** `on_trip` → `completed`, unchanged.
- An `on_trip` rider is never moved while aboard; only a planned handoff dropoff ends the current leg mid-trip.

## Atomic batch validation (guaranteed onward pickup)

Core never chooses the next car. **v1 choice:** install the onward plan only after the handoff dropoff (via a matcher call once the rider is `waiting_for_handoff`); there are no deferred/opaque pending plans that could overwrite other work. It validates the **complete matcher batch** against the **proposed post-batch state** (every proposed plan applied together; unchanged cars keep their current plans):

1. Every handoff dropoff for request R has at most one onward pickup for R in the post-batch plans. Zero is allowed (assigned after dropoff); two or more → invalid. At most one pickup globally per request, except origin pickup + one onward at the handoff location.
2. No duplicate final dropoffs for R across cars; a request appears in at most one plan for each of pickup and final dropoff.
3. Every handoff pickup location equals R's current handoff location; every handoff dropoff location differs from the destination.
4. At most one outstanding onward pickup per rider (v1). Existing seat, pooling, reassignment and aboard-lock rules still apply per plan.

**All-or-nothing.** If any linked handoff (or any other rule) fails, **roll back every plan change in that batch**—not only the first failing car. Otherwise an onward pickup can survive without its handoff. On rejection: each supply that had a proposed change in the batch emits one `plan_rejected` (sorted by `supply_id`); every supply keeps its **previous** plan; no `plan_updated`, no state transitions, no assignments. Router failures and `plan_declined` while applying a batch that includes a linked handoff likewise abort the whole batch the same way (decline or route error on one car rejects all linked changes).

**Onward car goes offline.** If an onward car goes offline before pickup, atomically remove its onward stops, leave the rider `waiting_for_handoff` but unassigned, emit `stop_removed`, and let subsequent matcher calls assign a replacement. The handoff timeout guarantees eventual resolution. This is a visible unassigned state, not an orphan. Do not require a same-batch replacement or force immediate cancellation.

## Handoff wait timeout and cancellation

Do **not** reuse `max_request_to_pickup_seconds` from `requested_at` for handoff waits (a long first leg would expire the wait immediately). Add optional setting `max_handoff_wait_seconds` (default off / unset), measured from each actual handoff dropoff until the next actual pickup. The deadline covers `waiting_for_handoff` **and** its assigned / `driver_arrived` onward states. When it fires, cancel with `cancel_reason = max_handoff_wait`, remove pending onward stops, and replan safely. Do not apply the original pickup limit again to later legs. Comparison B must configure and record a fixed value before running.

On any cancel of a waiting or onward-assigned rider: status → `canceled`; remove the linked onward stops (`stop_removed`); replan that supply; core owns cleanup. Existing wait limits still apply only to not-yet-boarded first-leg states. An `on_trip` rider is never canceled by wait limits. Canceling never invents a handoff.

## Outputs (metrics-ready, backward compatible)

Wholly non-handoff runs omit new columns and any new files (timezone precedent): existing consumers, bytes and `settings_sha256` stay unchanged. When handoffs are enabled or used in a comparison, the schema is **stable across all compared arms**, with empty cells where a trip had no handoff.

| Stream | Content |
|---|---|
| Trips | Always when `handoffs_enabled`: `handoff_count`, `total_handoff_wait_seconds` on `trips.csv`, plus a dedicated deterministic `trip_legs.csv` (`request_id`, `leg_index`, `supply_id`, start/end location, `pickup_at`, `dropoff_at`, `distance_meters`, `duration_seconds`). No encoded list column. Schema presence depends on explicit handoff configuration, not on whether a run happened to use a handoff |
| Supply events | Unchanged action names. Handoff dropoff is `dropoff` (rider becomes `waiting_for_handoff`); handoff pickup is `pickup`. First- and second-car arrival and departure at the handoff remain recoverable from `arrived_pickup` / `pickup` / `arrived_dropoff` / `dropoff` rows for those `request_id`s |

Determinism unchanged: no randomness, sorted iteration, reject-or-apply batches as a unit.

## Test matrix

| Case | Assert |
|---|---|
| Multi-car linked handoff, one plan invalid | Whole batch rolled back; every changed supply logs `plan_rejected`; prior plans unchanged; no orphan onward pickup |
| Onward car goes offline | Rider left `waiting_for_handoff` unassigned; onward stops removed; later rematch allowed; never an orphan |
| Handoff wait timeout | Cancels at `handoff_dropoff_at + max_handoff_wait_seconds`; onward stop removed; supply replanned |
| Duplicate onward pickup or final dropoff; wrong onward location; handoff at destination | Batch rejected |
| Capacity across handoff | Seats freed at handoff dropoff; onward car capacity enforced |
| Two-car successful handoff | Lifecycle and metrics: counts, waits, leg supply_ids, per-leg times/distances |
| Non-handoff golden runs | Byte-identical trips/events/summaries and `settings_sha256` vs today's goldens |

## Out of scope

Transfer selection/matching (`experiments/` only); OSRM; new seeds or fleet fractions; public materials; this design's implementation; walking between stops; pricing; any change to built-in pooling detour policy.
