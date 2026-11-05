# austin_weekday_100

100 RideAustin weekday ride requests from the busiest 30 minutes of the evening peak (18:30 to 19:00 on the synthetic day, Wed 2026-01-07, America/Chicago), cut down from [`austin_weekday_10k`](../austin_weekday_10k/README.md). It runs in well under a second with straight-line routing, so it suits quick checks, the viewer, and OSRM experiments.

## Source and license
Built only from `benchmarks/austin_weekday_10k/requests.csv` and `supply.csv`. Source and license are the same as that scenario (RideAustin, CC0 1.0 per the Mendeley Data copy).

## Method (build.py, seed 20161001)
1. Window: 18:30 to 19:00 local time (epoch 1767832200 to 1767834000). It is the busiest 30-minute window in the 10k scenario, with 358 requests.
2. Demand: 100 of those 358 requests are drawn uniformly without replacement with a fixed seed, so the geographic spread and the time of day inside the window follow the source. Origins, destinations and columns are unchanged.
3. Supply: 545 car shifts are online at some point from 18:15 (window start minus a 15-minute lead) to 19:00. Each one is kept with probability 100 / 358, which keeps the cars-per-request ratio of the source (1.52 in the source, 1.43 in the sample). Kept shifts are clipped so they go online no earlier than 18:15 and offline no later than 20:00 (window end plus a 1-hour buffer for trips to finish).
4. Extra columns are passed through unchanged, as in the 10k scenario.

## Stats
- Requests: 100. REGULAR 96, SUV 3, LUXURY 1.
- Supply: 143 car shifts.
- Full figures are in build_stats.json.

## Reference run

settings.toml is the same as the 10k scenario: `greedy_nearest_eta`, `straight_line` at 8 m/s, assignment timeout 300 s, pickup timeout 1200 s, dwells 30/15 s, pooling off, reassignment on, `timezone = "America/Chicago"`.

| routing | requests_completed | requests_canceled | median_wait_seconds | p90_wait_seconds | fleet_utilization | wall time |
|---|---|---|---|---|---|---|
| straight_line | 100 | 0 | 124 | 285 | 0.117586 | about 0.1 s |
| osrm (Austin, no candidate pruning, no cache) | 99 | 1 | 200 | 458 | 0.090035 | about 17 s |

## Caveats
Same as the 10k scenario. In addition, cars that were online before 18:15 start at their real shift start location rather than wherever they were at 18:15, and the clipped shift end can force cars offline while demand outside the window is not simulated.

## Rebuild, run and view

From the repo root (outputs are byte-identical; needs only Python 3):

```sh
python3 benchmarks/austin_weekday_100/build.py
go run ./cmd/rhs run -settings benchmarks/austin_weekday_100/settings.toml \
  -supply benchmarks/austin_weekday_100/supply.csv \
  -requests benchmarks/austin_weekday_100/requests.csv -output out/austin_100
go run ./cmd/rhs view out/austin_100
```

To use OSRM, copy settings.toml, set `routing = "osrm"` under `[hooks]`, and add an `[osrm]` section (see docs/spec.md).
