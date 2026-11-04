# ride-hailing-simulator: Tooling specification

A local web app for exploring simulation outputs, served by the `rhs` binary. It reads the output tables defined in [spec.md](spec.md#outputs), including `direct_duration_seconds` and `max_co_riders` in Trips and the run metadata table and never re-runs the simulation or calls the router.

## Contents

- [Goal and non-goals](#goal-and-non-goals)
- [Running the viewer](#running-the-viewer)
- [Conventions](#conventions)
- [Navigation and URLs](#navigation-and-urls)
- [1. Aggregate results dashboard](#1-aggregate-results-dashboard)
- [2. Trip view](#2-trip-view)
- [3. Supply view](#3-supply-view)
- [4. Run comparison](#4-run-comparison)
- [5. Matching decisions](#5-matching-decisions)
- [Decisions](#decisions)

## Goal and non-goals

- **Goal:** let someone who just ran a simulation see its results without writing code. That means checking aggregate quality, drilling into one trip or one car, and comparing two runs of the same inputs.
- **Non-goals:**
  - No hosting, accounts or auth. The viewer serves on localhost only.
  - No editing of inputs or outputs.
  - No live streaming of a running simulation.
  - No multi-user state.

## Running the viewer

```sh
rhs view -output out/ [-compare out2/] [-port 8080]
```

| Flag | Meaning |
|---|---|
| `-output` | Output directory of the run to view (required) |
| `-compare` | A second output directory; enables [run comparison](#4-run-comparison) |
| `-port` | Port to listen on; default `8080`. Binds to `127.0.0.1` only |

- The UI (HTML, JS, CSS) is embedded in the binary with `go:embed`, so there is nothing else to install. The server exposes the parsed outputs as JSON under `/api/...` and serves the UI for every other path.
- Maps use the Google Maps JavaScript API. The API key is read from the `GOOGLE_MAPS_API_KEY` environment variable or from `[viewer] google_maps_api_key` in a local settings file passed with `-settings`. The environment variable wins. An optional Map ID (`GOOGLE_MAPS_MAP_ID` or `[viewer] google_maps_map_id`) enables vector maps and advanced markers; without it the viewer uses raster maps and classic markers.
  - The key is never committed: fixtures and examples leave it out, and `.gitignore` covers a local `viewer.toml`.
  - The key is sent only to the browser page served on localhost and is never written to outputs or logs.
  - With no key, every page still works and maps are replaced by a notice.
- Outputs are read through the same reader plugins as the simulator, so non-CSV outputs work once a reader exists.

## Conventions

- Field names, units and timestamps follow [spec.md conventions](spec.md#conventions): `_seconds`, `_meters`, integer epoch seconds and signed decimal lat/lng. Times are shown as offsets from the run start (`T+654`) with the epoch value on hover.
- `Trips`, `SupplyEvents`, `SupplySummary` and `RunMetadata` mean the output tables of the same names. `completed` means trips with `status = completed`, and `N(x)` is the number of rows in `x`.
- Percentiles use the nearest-rank method, the same as the run summary.
- The Go server computes every aggregate and paginates long tables (100 rows per page by default); the browser never loads a whole table.
- A ratio with a zero denominator is shown as `n/a`, not 0.
- Every metric on the dashboard links to its definition in this document.

## Navigation and URLs

Every view has a stable URL, so a link can be pasted into a PR comment.

| URL | View |
|---|---|
| `/` | Aggregate dashboard |
| `/trips` | Trip list: sortable, filterable by status, `ridepool_type` and `cancel_reason` |
| `/trips/{request_id}` | Trip view |
| `/supply` | Supply summary list |
| `/supply/{supply_id}` | Supply view |
| `/supply/{supply_id}?at={epoch}` | Supply view with the timeline and map focused at a time |
| `/compare` | Run comparison (only with `-compare`) |
| `/compare/trips/{request_id}` | One trip in both runs, side by side |
| `/decisions` | Reserved for [matching decisions](#5-matching-decisions) |

A top bar links to the dashboard, Trips, Supply and Compare. A `request_id` or `supply_id` shown anywhere is a link to its view.

## 1. Aggregate results dashboard

Numbers come with a small distribution chart where noted. Definitions:

### Supply utilization

Let `online_seconds = (offline_at or run end) - online_at` for each row of SupplySummary, and let `total_meters = idle_meters + en_route_meters + on_trip_meters`. Run end is `RunMetadata.simulation_end_at`.

| Metric | Definition |
|---|---|
| `on_trip_time_share` | `sum(on_trip_seconds) / sum(online_seconds)` |
| `en_route_time_share` | `sum(en_route_seconds) / sum(online_seconds)` |
| `idle_time_share` | `sum(idle_seconds) / sum(online_seconds)` |
| `on_trip_distance_share` | `sum(on_trip_meters) / sum(total_meters)` |
| `en_route_distance_share` | `sum(en_route_meters) / sum(total_meters)` |
| `idle_distance_share` | `sum(idle_meters) / sum(total_meters)` (repositioning) |
| `deadhead_distance_share` | `en_route_distance_share + idle_distance_share` |
| `utilization` distribution | Histogram of per-car `utilization` |

The time shares add up to 1. Time spent `going_offline` counts in whichever of the three it was doing. A stacked bar shows the three time shares and the three distance shares.

### Rider wait

| Metric | Definition |
|---|---|
| `wait_seconds` mean, median, p90, max | Over completed trips: `pickup_at - requested_at` |
| Wait distribution | Histogram of `wait_seconds` in 60 s buckets |
| `eta_at_assignment_seconds` mean, median, p90 | Over trips with `assigned_at` |
| `assignment_delay_seconds` mean, median, p90 | Over trips with `first_assigned_at`: `first_assigned_at - requested_at` |
| `eta_error_seconds` mean, median, p90 | Over completed trips: `driver_arrived_at - (assigned_at + eta_at_assignment_seconds)`. It's 0 with the straight-line router unless the plan changed; positive means the car arrived late |

### Efficiency

| Metric | Definition |
|---|---|
| `rider_meters_per_supply_meter` | `sum(direct_distance_meters over completed) / sum(total_meters)`. It's above 1 only when pooling saves distance |
| `rider_meters_driven_per_supply_meter` | `sum(trip_distance_meters over completed) / sum(total_meters)` |
| `pickup_distance_meters` mean, median, p90 | Over completed trips |
| `requests_served_per_supply_hour` | `N(completed) / (sum(online_seconds) / 3600)` |

### Completion and cancellations

Rates are over all rows of Trips.

| Metric | Definition |
|---|---|
| `completion_rate` | `N(completed) / N(Trips)` |
| `cancel_rate` | `N(status = canceled) / N(Trips)` |
| `cancel_rate_max_request_to_assignment` | `N(cancel_reason = max_request_to_assignment) / N(Trips)` |
| `cancel_rate_max_request_to_pickup` | `N(cancel_reason = max_request_to_pickup) / N(Trips)` |
| `unserved_rate` | Rows still open at the end of the run (any other status) `/ N(Trips)` |
| `cancel_after_assignment_share` | Of canceled trips, the share with `first_assigned_at` set (a car was spent on them) |

### Pooling

A completed trip is **shared** if `max_co_riders > 0`: at any moment while its rider was aboard, another rider was aboard too. This uses `max_co_riders` rather than `ridepool_type`, which doesn't show riders who boarded before this trip's `assigned_at` and got out after its dropoff. A trip can be both shared and forward-dispatched.

| Metric | Definition |
|---|---|
| `shared_trip_rate` (match rate) | `N(shared completed) / N(completed)` |
| `forward_dispatch_rate` | Completed trips whose `ridepool_type` has a `d` before `p0`, `/ N(completed)` |
| `ridepool_type` breakdown | Count and share of each distinct `ridepool_type` over completed trips, most common first. Each row links to `/trips?ridepool_type=...` |
| `max_co_riders` distribution | Histogram of `max_co_riders` over completed trips |
| `max_riders_aboard` distribution | Per car, histogram of the largest number of riders aboard at once: requests picked up but not yet dropped off, counted from SupplyEvents pickup and dropoff rows |

### Rider inconvenience

All over completed trips. Dwell time is left out of every ratio, so `dropoff_dwell_seconds` is subtracted from `trip_duration_seconds`.

| Metric | Definition |
|---|---|
| `extra_meters` mean, median, p90 | `trip_distance_meters - direct_distance_meters` |
| `detour_ratio` mean, median, p90 | `trip_distance_meters / direct_distance_meters`, from Trips |
| `extra_seconds` mean, median, p90 | `trip_duration_seconds - direct_duration_seconds - dropoff_dwell_seconds` |
| `duration_ratio` mean, median, p90 | `(trip_duration_seconds - dropoff_dwell_seconds) / direct_duration_seconds` |
| `total_time_ratio` mean, median, p90 | `(dropoff_at - requested_at - dropoff_dwell_seconds) / direct_duration_seconds`: the whole wait plus ride against an instant direct trip |

`dropoff_dwell_seconds` comes from `RunMetadata.settings`.

### Other

| Metric | Definition |
|---|---|
| `reassignment_rate` | `N(reassign_count > 0) / N(trips with first_assigned_at)` |
| `reassignments_per_trip` | `sum(reassign_count) / N(trips with first_assigned_at)` |
| `plan_rejected_count` | `N(SupplyEvents where action = plan_rejected)`. Shown in red if above 0, since the reference matcher should never be rejected |
| Matcher calls by trigger | From the run summary |
| Requests and online supply over time | Line chart in 60 s buckets: open requests (requested, assigned or driver_arrived) and cars by state. Built from Trips timestamps and SupplyEvents `state_after` |

## 2. Trip view

`/trips/{request_id}`

- **Map:**
  - The car's path from `assigned_at` to `dropoff_at`: the `driven_polyline` cells of `supply_id`'s SupplyEvents rows in that window.
  - The path to the pickup is drawn dashed and the path with the rider aboard solid.
  - Markers for the car's location at `assigned_at`, the origin (pickup) and the destination (dropoff).
  - A thin grey straight line for the direct origin-to-destination route.
  - For a canceled trip, the planned pickup route from the last `plan_updated` row before `canceled_at` and the point where the car was at cancellation.
- **Metrics:**
  - Every Trips field for the row, grouped as times, distances and assignment, with times as `T+` offsets.
  - Derived: `extra_meters`, `extra_seconds`, `assignment_delay_seconds` and `eta_error_seconds`.
- **Ridepool:**
  - `ridepool_type` rendered as a token sequence (for example `p0 p1 d1 d0`), with each token labeled by its `request_id`. Labels come from matching the car's pickup and dropoff rows after `assigned_at`.
  - Tokens other than `0` link to the co-rider's trip view, and a `shared` badge shows if `max_co_riders > 0`.
- **Links:** the supply view at the assignment time (`/supply/{supply_id}?at={assigned_at}`). With reassignment, a list of earlier cars, from SupplyEvents `plan_updated` rows whose `plan` contains this request.
- **Compare:** with `-compare`, a link to `/compare/trips/{request_id}`.

## 3. Supply view

`/supply`: one row per SupplySummary row, showing every column plus `online_seconds` and `total_meters`. Sortable; each `supply_id` links to its view.

`/supply/{supply_id}`

- **Map:** the car's driven path, one polyline per SupplyEvents row. Each row's `driven_polyline` was driven in the previous row's `state_after`, so it's colored by that state:

  | State | Color |
  |---|---|
  | `idle` | grey |
  | `en_route` | blue |
  | `on_trip` | green, darker for 2+ riders aboard |
  | `going_offline` | orange |

  Markers show each pickup and dropoff, labeled with the `request_id`. Hovering a stop highlights its timeline row.
- **Stats:** the SupplySummary row, plus `online_seconds`, `max_riders_aboard` and `plan_rejected_count` for this car.
- **Timeline:**
  - Every SupplyEvents row in order: `T+` time, `action`, `state_after`, `request_id` (linked), `seats_in_use` and `odometer_meters`.
  - `plan_updated` rows expand to show the plan's stops with `eta_at`, and draw `planned_polyline` dashed on the map.
  - A state band above the timeline shows time in each state.
  - `?at=` scrolls to that time and puts the car marker at its interpolated position, using the interpolation rule in spec.md.
- **Trips:** a list of every request assigned to this car at any time, linked, with status and `ridepool_type`. Requests the car lost to reassignment are marked `reassigned away`.

## 4. Run comparison

`rhs view -output A/ -compare B/` serves `/compare`. A is the baseline, B the candidate. Only two runs are compared for now; several runs against one baseline is planned for later.

- **Paired check.** A comparison is paired only if both runs used the same inputs. The viewer compares `requests_sha256` and `supply_sha256` from each run's [run metadata](spec.md#5-run-metadata-one-row), which hash the parsed records so reformatting a CSV doesn't break pairing.
  - If the hashes match, the header shows **Paired: identical requests and supply**.
  - If they differ, a red banner says the comparison is **not paired**, and per-trip comparison is turned off.
  - Settings count toward the comparison but don't block it, since comparing settings is usually the point. If `settings_sha256` differs, a settings diff table built from `RunMetadata.settings` lists every setting and hook whose value differs. A `simulator_version` mismatch shows a warning.
- **Aggregate table:**
  - Every [dashboard](#1-aggregate-results-dashboard) metric is shown with `A`, `B`, `delta = B - A` and `percent_delta = (B - A) / A * 100` (shown as `n/a` when `A = 0`).
  - Each metric declares whether higher is better (for example `completion_rate`) or lower is better (for example `wait_seconds` p90), so deltas are colored green or red. Neutral metrics, such as matcher calls, aren't colored.
- **Distributions:** wait, `detour_ratio` and utilization histograms are overlaid for A and B.
- **Paired trips:**
  - When the comparison is paired, a table joins A and B Trips on `request_id`. It shows status in A and B and `wait_seconds_delta`, plus `trip_distance_meters_delta` and `detour_ratio_delta` for trips completed in both.
  - The table can be sorted to find the biggest regressions, and filtered to trips whose status changed.
  - Each row links to `/compare/trips/{request_id}`, which shows both trip views side by side with linked map zoom.
- **Fleet:** SupplySummary is joined on `supply_id` with per-car deltas of utilization and `total_meters`.

## 5. Matching decisions

TBD. This section will be specified in a later commit. `/decisions` is reserved for it.

## Decisions

1. **Pairing:** the input hashes cover the parsed records, not the raw files. Settings count toward pairing: a different `settings_sha256` is shown as a settings diff but doesn't block the comparison.
2. **Shared:** a trip is shared if another rider was aboard at any moment of it (`max_co_riders > 0`). No minimum overlap.
3. **Forward dispatch rate:** counts only completed trips. A trip can be both forward-dispatched and shared.
4. **Dwell:** left out of every inconvenience ratio.
5. **Commands:** `rhs view` and `rhs run` are subcommands of one binary.
6. **Scale:** the Go server aggregates and paginates; the browser gets only what a page shows.
7. **Maps:** an API key is required for maps, and an optional Map ID enables vector maps and advanced markers.
8. **Comparison:** two runs now; several runs against one baseline later.
