# ride-hailing-simulator: Benchmark specification

Benchmark scenarios are fixed, versioned input sets built from public trip data, so that matchers and settings can be compared on the same realistic demand and supply. A scenario contains only simulator inputs ([spec.md](spec.md#inputs-and-settings)); the source data's matches and routes are not used except for optional calibration.

## Contents

- [What a scenario is](#what-a-scenario-is)
- [Layout and naming](#layout-and-naming)
- [Reproducibility](#reproducibility)
- [Sampling method](#sampling-method)
- [Licensing and provenance](#licensing-and-provenance)
- [Required stats and reference run](#required-stats-and-reference-run)
- [Extra columns](#extra-columns)
- [Caveats](#caveats)
- [Catalog](#catalog)
- [Decisions](#decisions)

## What a scenario is

A scenario is one directory with:

| File | Contents |
|---|---|
| `requests.csv` | Ride requests in the [demand format](spec.md#demand-ride-requests) |
| `supply.csv` | Cars in the [supply format](spec.md#supply-cars) |
| `settings.toml` | The reference settings the published results were run with |
| `README.md` | Source, license, method, stats, reference run and caveats |
| `build.py` | Deterministic build script from raw data to the files above |
| `build_stats.json` | Machine-readable build stats (see below) |
| `profile.png` | Optional: demand and supply by time of day |

Raw source data is never committed. The README says where to get it and gives its sha256.

## Layout and naming

- Path: `benchmarks/<name>/`.
- `<name>` is lowercase snake_case: `<city>_<day type>_<size>`, for example `austin_weekday_10k`. A different sample, window or method is a new name, never an edit in place; a fix that changes outputs adds a version suffix (`_v2`).
- IDs in the CSVs are scenario-local and deterministic; source IDs, if kept, go in extra columns.

## Reproducibility

- **Fixed seed.** One integer seed, recorded in `build.py` and `build_stats.json`. Every random draw uses it.
- **Raw-data hashes.** The README lists the sha256 of every raw input file. `build.py` should check them.
- **Deterministic build.** The same raw files and seed produce byte-identical `requests.csv`, `supply.csv` and `build_stats.json`. Sorts must be stable with explicit tie-breaks.
- **Synthetic date.** Trips sampled from many source days are mapped onto one synthetic day by keeping their time of day relative to their source day's local midnight. The README states the synthetic date and its midnight epoch.
- **Timezone.** Times of day are taken in the source city's local timezone (IANA name in the README); CSV timestamps stay epoch seconds per [spec.md conventions](spec.md#conventions).
- **Inputs, not results.** The scenario is identified by the `requests_sha256` and `supply_sha256` of a run, which also make paired comparisons work in the viewer.

## Sampling method

Scenarios built from multi-day trip data follow these rules unless the README says otherwise and why:

1. **Day filter.** Use one day type (for example Mon–Fri). Exclude public holidays, holiday weeks and large events (for example SXSW), and early periods when service was still ramping up.
2. **Demand profile.** Allocate the target request count across (15-minute slot × requested vehicle category) cells by largest remainder of the filtered pool's distribution, then draw trips uniformly at random within each cell. Each trip keeps its real origin, destination and time of day.
3. **Supply from driver shifts.** Chain each driver's trips over the whole source timeline (across midnight); a gap longer than a threshold (1 h by default) starts a new shift. A shift goes online at its first request minus a lead time (5 min by default) at its first pickup and goes offline at its last dropoff.
4. **Shift thinning.** For each sampled trip, keep its real shift with probability 1/(trips in that shift). This is a Horvitz–Thompson estimate that keeps the shift count and timing at the sample's scale without favoring long shifts. Shifts are mapped onto the synthetic day like the trips, so they may start before 00:00 or end after 24:00.
5. **No driver IDs.** If the source has no driver IDs, size a fleet from peak demand with a recorded seed and document it.

## Licensing and provenance

- The source must allow redistribution of derived data. Record the license and where it was confirmed; if the original page is gone, cite a mirror that states it.
- The README names the dataset, the publisher, download URLs (and mirrors used), the raw file hashes and the license.
- Datasets with unclear licenses stay planned until cleared.

## Required stats and reference run

`build_stats.json` includes at least: seed, number of pool days and trips, requests, supply rows, distinct source drivers, requests by category, supply by seats, requests by hour, peak online cars and peak active trips with their local times.

The README publishes a reference run: the `settings.toml` used, the commit it ran on, and the `run_summary.csv` values `requests_completed`, `requests_canceled` (by reason), `requests_unserved`, `median_wait_seconds`, `p90_wait_seconds`, `mean_detour_ratio` and `fleet_utilization`. Timing columns are not published. A change to the simulator that changes these numbers updates the README in the same PR.

## Extra columns

Scenario CSVs may carry extra columns; the CSV reader looks columns up by header name and ignores unknown ones. Current extras:

- requests: `vehicle_category`, `source_requested_at_local`, `source_driver_id`
- supply: `vehicle_categories` (`|`-separated), `source_driver_id`, `source_trips_in_shift`

`vehicle_category` and `vehicle_categories` are informational until type-aware matching is specified in [spec.md](spec.md).

## Caveats

- Public trip data usually holds completed trips only, so canceled and unserved demand is missing.
- Driver idle time before the first and after the last trip is not recorded, so shifts are a lower bound on time online.
- Source coordinates may be rounded (about 100 m for RideAustin) or zone-level.
- Results with `straight_line` routing are directional; real-road routing will change wait times.

## Catalog

| Name | Source | Status |
|---|---|---|
| [`austin_weekday_10k`](../benchmarks/austin_weekday_10k/README.md) | RideAustin, Oct 2016–Apr 2017 weekdays; 10,000 requests, 3,377 shifts | Available |
| [`austin_weekday_100`](../benchmarks/austin_weekday_100/README.md) | Cut from `austin_weekday_10k`: 100 requests drawn with seed 20161001 from 18:30–19:00 local time (the busiest 30 minutes); car shifts online from 18:15 to 19:00 kept with probability 100 / requests in window (143 shifts), clipped to 18:15–20:00 | Available |
| `sf_cabspotting_*` | CRAWDAD epfl/mobility, ~536 SF cabs, May–Jun 2008, CC BY | Planned |
| `nyc_manhattan_week` | NYC TLC trip records / FleetPy Manhattan week (Zenodo 15187906) | Planned |
| `chicago_tnp_week` | Chicago TNP trips / FleetPy June 2022 week (Zenodo 15189440) | Planned |
| `porto_*` | ECML/PKDD 2015 Porto taxi trajectories, 442 taxis | Planned (license unclear) |

## Decisions

1. **Inputs only:** scenarios ship requests, supply and settings; source matches and routes are used only for calibration.
2. **No raw data in git:** raw files are referenced by URL and sha256 and rebuilt with `build.py`.
3. **One synthetic day:** multi-day samples are collapsed onto one synthetic day in the source's local timezone.
4. **Shift thinning:** keep probability 1/(trips in shift), so supply matches the sample's scale.
5. **Vehicle categories:** carried as extra columns, ignored by the simulator until type-aware matching is specified.
6. **Immutability:** a scenario's outputs never change under the same name.
