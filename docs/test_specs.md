# ride-hailing-simulator: Test specification

All tests are component tests, as required in [spec.md](spec.md#testing). Each test runs one full simulation from a settings file, a supply file and a demand file, then asserts on the outputs.

## Contents

- [Fixture layout](#fixture-layout)
- [Naming conventions](#naming-conventions)
- [Landmarks](#landmarks)
- [Reference test matcher](#reference-test-matcher)
- [How expected values are derived](#how-expected-values-are-derived)
- [Test cases](#test-cases)

## Fixture layout

```
tests/fixtures/
├── settings/   # one TOML file per settings variation
├── supply/     # supply CSVs
└── demand/     # ride request CSVs
```

A test names one file from each folder. Files are shared: most tests reuse `base_default.toml`, `one_car_at_kezar_stadium.csv` or `single_trip.csv`. Add a new file only when no existing one fits.

Each settings file is complete on its own (no inheritance), so a reader can see every value in one place. Settings that are off are left out, because TOML has no null. Reader and writer paths are set by the test harness, not the fixtures.

## Naming conventions

| Item | Convention | Example |
|---|---|---|
| File names | Lowercase `snake_case` describing the scenario, not the test | `simple_forward_dispatch.csv` |
| Settings files | The one thing that differs from the baseline | `short_pickup_timeout.toml` |
| Supply files | Number of cars and where they start, plus any timing twist | `one_car_online_late_at_kezar_stadium.csv` |
| Demand files | The scenario shape | `simple_pool.csv` |
| `request_id` | `<origin landmark>_to_<destination landmark>` | `kezar_stadium_to_coit_tower` |
| `supply_id` | `car_at_<starting landmark>` | `car_at_ferry_building` |
| Test names | `snake_case` sentence of the behavior | `pooling_off_serves_requests_one_after_another` |
| Times | Base time `T = 1767225600` (2026-01-01 00:00:00 UTC) plus small offsets | `T+60 = 1767225660` |

Optional CSV columns are always present in fixtures (empty when unset), so headers match the spec exactly.

## Landmarks

| Landmark | `lat` | `lng` |
|---|---|---|
| `kezar_stadium` | 37.767263 | -122.456743 |
| `coit_tower` | 37.802395 | -122.405822 |
| `ferry_building` | 37.795490 | -122.393680 |
| `union_square` | 37.787994 | -122.407437 |
| `painted_ladies` | 37.776120 | -122.432700 |
| `oracle_park` | 37.778595 | -122.389270 |
| `chase_center` | 37.768019 | -122.387569 |
| `palace_of_fine_arts` | 37.802816 | -122.448391 |
| `twin_peaks` | 37.751586 | -122.447722 |
| `lombard_street` | 37.802139 | -122.418740 |
| `conservatory_of_flowers` | 37.772031 | -122.460217 |
| `de_young` | 37.771470 | -122.468658 |

The first five are used by the current fixtures; the rest are reserved for future tests. Coordinates were checked against Wikipedia and OpenStreetMap to within about 100 m.

## Reference test matcher

`reference_test_matcher` is a simple, deterministic matcher used **only by tests**. It is not a recommended production strategy.

1. Consider open requests in `requested_at` order, then by `request_id`. With reassignment off, only `requested` (unassigned) requests are considered. With reassignment on, assigned requests not yet picked up are also considered.
2. For each request, evaluate every online supply that isn't `going_offline`, in `supply_id` order:
   - **Pooling off:** append pickup and dropoff to the end of the car's plan (forward dispatch when the plan isn't empty).
   - **Pooling on:** try every valid insertion of the pickup and dropoff into the remaining plan, keeping existing stop order and seat limits, and keep the one with the lowest total plan duration. Ties go to the earliest positions.
3. Choose the car with the earliest pickup ETA. Ties go to the lowest `supply_id`. An assigned request moves only if another car is strictly earlier.
4. Return a plan change for every car whose plan changed; return nothing if no plan changed.

## How expected values are derived

- Distance is haversine (mean Earth radius 6,371,008.8 m), rounded to whole meters. Duration is the rounded distance divided by 10 m/s (`average_speed_meters_per_second = 10.0`), rounded to whole seconds.
- Mid-leg positions are linearly interpolated along the straight segment.
- All times below are offsets from `T = 1767225600`.

| Leg | `distance_meters` | `duration_seconds` |
|---|---|---|
| kezar_stadium → coit_tower | 5940 | 594 |
| union_square → kezar_stadium | 4908 | 491 |
| ferry_building → kezar_stadium | 6369 | 637 |
| coit_tower → ferry_building | 1314 | 131 |
| coit_tower → painted_ladies | 3757 | 376 |
| painted_ladies → union_square | 2583 | 258 |
| coit_tower → kezar_stadium | 5940 | 594 |
| union_square → coit_tower | 1608 | 161 |

## Test cases

Unless noted, every case uses `base_default.toml`. Every settings fixture sets `default_num_requested_seats = 1` explicitly (the spec default is 4), and every demand fixture also states `num_requested_seats` per request.

Cases 1 and 4 are the **exhaustive output tests**: they assert every field of every output. All other cases are **behavior tests** and assert only the few values their behavior is about.

### 1. `single_trip_from_car_at_origin` (exhaustive)

- **Purpose:** Verifies every output field on the simplest trip. The car is already at the origin, so pickup is immediate.
- **Fixtures:** `base_default.toml`, `one_car_at_kezar_stadium.csv`, `single_trip.csv`
- **Trip `kezar_stadium_to_coit_tower`:** `status = completed`, `requested_at = T+60`, `num_requested_seats = 1`, `first_assigned_at = assigned_at = T+60`, `supply_id = car_at_kezar_stadium`, `reassign_count = 0`, `ridepool_type = p0d0`, `eta_at_assignment_seconds = 0`, `driver_arrived_at = pickup_at = T+60`, `dropoff_at = T+654`, `wait_seconds = 0`, `trip_distance_meters = 5940`, `trip_duration_seconds = 594`, `direct_distance_meters = 5940`, `direct_duration_seconds = 594`, `detour_ratio = 1.0`, `pickup_distance_meters = 0`, `max_co_riders = 0`, `canceled_at` and `cancel_reason` empty.
- **Event log:** `online` (T), `plan_updated` (T+60), `arrived_pickup` (T+60), `pickup` (T+60), `arrived_dropoff` (T+654), `dropoff` (T+654), `idle` (T+654), each with `lat`, `lng`, `state_after`, `request_id`, `plan_id`, `seats_in_use` and `odometer_meters` (0 until the dropoff rows, then 5940). The polyline and `plan` cells match the router output.
- **Supply summary:** `online_at = T`, `offline_at` empty, `idle_seconds = 60`, `en_route_seconds = 0`, `on_trip_seconds = 594`, `idle_meters = 0`, `en_route_meters = 0`, `on_trip_meters = 5940`, `requests_served = 1`, `utilization = 594 / 654`, `deadhead_ratio = 0`, `full_polyline` from Kezar Stadium to Coit Tower.
- **Run metadata:** `simulator_version`, the three SHA-256 hashes, `settings` JSON, `simulation_start_at = T+60`, `simulation_end_at = T+654`.
- **Run summary:** 1 completed, 0 canceled, median and p90 wait 0, mean detour ratio 1.0, 1 matcher call with trigger `request` (plus 0 for each other trigger).

### 2. `idle_car_drives_to_pickup`

- **Fixtures:** `base_default.toml`, `one_car_at_union_square.csv`, `single_trip.csv`
- **Assert:** `pickup_at = T+551` (`wait_seconds = 491`), `pickup_distance_meters = 4908`.

### 3. `simple_forward_dispatch`

- **Fixtures:** `base_default.toml`, `one_car_at_kezar_stadium.csv`, `simple_forward_dispatch.csv`
- **Assert:** `coit_tower_to_ferry_building` has `ridepool_type = d1p0d0` and `pickup_at = T+654`. At T+654 the first request's `dropoff` is logged before the second's `pickup`.

### 4. `simple_pool` (exhaustive)

- **Purpose:** Verifies every output field when two riders share a car, including the detour fields.
- **Fixtures:** `pooling_enabled.toml`, `one_car_at_kezar_stadium.csv`, `simple_pool.csv`
- **Plan:** at T+120 the car is 600 m along its route, and the plan becomes pickup `painted_ladies_to_union_square`, dropoff `painted_ladies_to_union_square`, dropoff `kezar_stadium_to_coit_tower` (595 s, against 713 s and 1168 s for the other insertions).
- **Trip `painted_ladies_to_union_square`:** `requested_at = assigned_at = T+120`, `eta_at_assignment_seconds = 176`, `driver_arrived_at = pickup_at = T+296`, `wait_seconds = 176`, `pickup_distance_meters = 1763`, `dropoff_at = T+554`, `trip_distance_meters = direct_distance_meters = 2583`, `trip_duration_seconds = 258`, `direct_duration_seconds = 258`, `detour_ratio = 1.0`, `ridepool_type = p0d0`, `max_co_riders = 1`.
- **Trip `kezar_stadium_to_coit_tower`:** as in case 1, except `ridepool_type = p0p1d1d0`, `dropoff_at = T+715`, `trip_distance_meters = 6554`, `trip_duration_seconds = 655`, `direct_duration_seconds = 594`, `detour_ratio = 6554 / 5940`, `max_co_riders = 1`.
- **Event log and summaries:** every row and field is asserted, as in case 1. Max `seats_in_use = 2`, final `odometer_meters = 6554`, `on_trip_meters = 6554`, `requests_served = 2`.

### 5. `pooling_off_serves_requests_one_after_another`

- **Fixtures:** `base_default.toml`, `one_car_at_kezar_stadium.csv`, `simple_pool.csv`
- **Assert:** `painted_ladies_to_union_square` has `ridepool_type = d1p0d0` and `pickup_at = T+1030`, `max_co_riders = 0`. `seats_in_use` never exceeds 1.

### 6. `party_of_four_cannot_pool`

- **Fixtures:** `pooling_enabled.toml`, `one_car_at_kezar_stadium.csv`, `simple_pool_second_rider_party_of_four.csv`
- **Assert:** `painted_ladies_to_union_square` has `ridepool_type = d1p0d0` (1 + 4 seats exceeds 4).

### 7. `rider_cancels_when_no_car_is_online`

- **Fixtures:** `short_assignment_timeout.toml`, `one_car_online_late_at_kezar_stadium.csv`, `single_trip.csv`
- **Assert:** `status = canceled`, `cancel_reason = max_request_to_assignment`, `canceled_at = T+180`.

### 8. `rider_cancels_when_pickup_takes_too_long`

- **Fixtures:** `short_pickup_timeout.toml`, `one_car_at_ferry_building.csv`, `single_trip.csv`
- **Assert:** `cancel_reason = max_request_to_pickup`, `canceled_at = T+360`. The car logs `stop_removed` then `idle` at T+360.

### 9. `per_request_pickup_limit_overrides_setting`

- **Fixtures:** `short_pickup_timeout.toml`, `one_car_at_ferry_building.csv`, `single_trip_with_pickup_timeout_override.csv`
- **Assert:** `status = completed`, `pickup_at = T+697` (past the 300 s setting, within the 900 s override).

### 10. `dwell_times_delay_pickup_and_dropoff`

- **Fixtures:** `with_dwell_times.toml`, `one_car_at_kezar_stadium.csv`, `single_trip.csv`
- **Assert:** `pickup_at = T+90`, `dropoff_at = T+704`, `trip_duration_seconds = 614` (594 driving plus the 20 s dropoff dwell).

### 11. `car_goes_offline_after_finishing_plan`

- **Fixtures:** `base_default.toml`, `one_car_at_kezar_stadium_offline_early.csv`, `single_trip.csv`
- **Assert:** the trip is `completed`; the car's last rows are `dropoff` then `offline` at T+654.

### 12. `closer_car_takes_over_when_reassignment_enabled`

- **Fixtures:** `base_default.toml`, `ferry_building_car_then_union_square_car_online_late.csv`, `single_trip.csv`
- **Assert:** `supply_id = car_at_union_square`, `reassign_count = 1`, `first_assigned_at = T+60`, `assigned_at = T+120`. At T+120, `car_at_ferry_building` is 577 s away and `car_at_union_square` is 491 s away.

### 13. `assigned_car_keeps_request_when_reassignment_disabled`

- **Fixtures:** `reassignment_disabled.toml`, `ferry_building_car_then_union_square_car_online_late.csv`, `single_trip.csv`
- **Assert:** `supply_id = car_at_ferry_building`, `reassign_count = 0`.

### 14. `batch_ticks_without_changes_are_no_ops`

- **Fixtures:** `batch_every_60_seconds.toml`, `one_car_at_union_square.csv`, `single_trip.csv`
- **Assert:** exactly one `plan_updated` row; trips are identical to case 2.

### 15. `supply_online_runs_before_request_at_same_second`

- **Fixtures:** `base_default.toml`, `one_car_online_with_first_request_at_kezar_stadium.csv`, `single_trip.csv`
- **Assert:** at T+60, `online` is logged before `plan_updated`, and `wait_seconds = 0`.

### 16. `same_inputs_give_identical_outputs`

- **Fixtures:** `pooling_enabled.toml`, `one_car_at_kezar_stadium.csv`, `simple_pool.csv`
- **Assert:** two runs produce byte-identical trips, supply event log and supply summary (run summary excluding timings), and identical run metadata.

### 17. `input_hashes_pair_runs_with_different_settings`

- **Fixtures:** `pooling_enabled.toml` and `base_default.toml`, each with `one_car_at_kezar_stadium.csv` and `simple_pool.csv`
- **Assert:** `requests_sha256` and `supply_sha256` match across the two runs; `settings_sha256` differs.

### Driver behavior

Cases 18 to 23 are in `tests/driver_behavior_test.go`. `return_to_origin.toml` is `base_default.toml` plus `[driver_behavior] plugin = "return_to_origin"`, and `simple_driver_behavior.toml` is `base_default.toml` plus `plugin = "simple"`.

### 18. `simple_driver_behavior_is_the_default`

- **Fixtures:** `base_default.toml` and `simple_driver_behavior.toml`, each with `one_car_at_union_square.csv` and `single_trip.csv`
- **Assert:** both runs produce byte-identical trips, supply event log, supply summary, run summary (excluding timings) and run metadata, including `settings_sha256`. Every earlier case runs with the default and is unchanged.

### 19. `car_returns_to_origin_after_dropoff`

- **Fixtures:** `return_to_origin.toml`, `one_car_at_kezar_stadium.csv`, `single_trip.csv`
- **Assert:** after `dropoff` and `idle` at T+654, there's a `reposition_started` row at Coit Tower (`state_after = repositioning`, `planned_polyline` set, `plan` empty). `reposition_arrived` follows at T+1248 at Kezar Stadium (`state_after = idle`, `odometer_meters = 11880`). Supply summary: `idle_seconds = 654` (60 + 594), `idle_meters = 5940`, `on_trip_meters = 5940`. `simulation_end_at = T+1248`.

### 20. `car_is_assigned_mid_reposition_from_interpolated_position`

- **Fixtures:** `return_to_origin.toml`, `one_car_at_kezar_stadium.csv`, `second_trip_requested_mid_return.csv` (`kezar_stadium_to_coit_tower` at T+60, then `coit_tower_to_ferry_building` at T+954)
- **Assert:** at T+954 the car is 300 s, or 3000 m, into its return. `plan_updated` at T+954 is logged at (37.784652, -122.431540) with `odometer_meters = 8940` and `state_after = en_route`. `coit_tower_to_ferry_building` has `eta_at_assignment_seconds = 300`, `pickup_at = T+1254` and `pickup_distance_meters = 3000`. After its dropoff, the car repositions again from the Ferry Building: 2 `reposition_started` rows and 1 `reposition_arrived`. `idle_meters = 9369` (3000 + 6369), `en_route_meters = 3000`.

### 21. `no_reposition_when_already_at_origin`

- **Fixtures:** `return_to_origin.toml`, `one_car_at_kezar_stadium.csv`, `round_trip_back_to_kezar_stadium.csv` (Kezar Stadium to Coit Tower at T+60, then Coit Tower to Kezar Stadium at T+61, forward-dispatched)
- **Assert:** the last row is `idle` at T+1248 at Kezar Stadium, with no `reposition_started` rows and `idle_meters = 0`.

### 22. `car_goes_offline_during_reposition`

- **Fixtures:** `return_to_origin.toml`, `one_car_at_kezar_stadium_offline_during_return.csv` (`offline_at = T+954`), `single_trip.csv`
- **Assert:** the last row is `offline` at T+954 at the interpolated position (37.784652, ...), with `odometer_meters = 8940` and no `reposition_arrived`. Supply summary `offline_at = T+954`, `idle_meters = 3000`.

### 23. `going_offline_car_does_not_reposition`

- **Fixtures:** `return_to_origin.toml`, `one_car_at_kezar_stadium_offline_early.csv`, `single_trip.csv`
- **Assert:** the last row is `offline` at T+654 at Coit Tower, with no `reposition_started` rows.
