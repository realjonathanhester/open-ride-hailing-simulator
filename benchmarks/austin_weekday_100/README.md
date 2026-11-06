# austin_weekday_100

100 RideAustin weekday ride requests from the busiest 30 minutes of the evening peak (18:30 to 19:00 on the synthetic day, Wed 2026-01-07, America/Chicago), cut down from [`austin_weekday_10k`](../austin_weekday_10k/README.md). It runs in well under a second with straight-line routing, so it suits quick checks, the viewer, and OSRM experiments.

## Source and license
Built only from `benchmarks/austin_weekday_10k/requests.csv` and `supply.csv`. Source and license are the same as that scenario (RideAustin, CC0 1.0 per the Mendeley Data copy).

## Method (build.py, seed 20161001)
1. Window: 18:30 to 19:00 local time (epoch 1767832200 to 1767834000). It is the busiest 30-minute window in the 10k scenario, with 358 requests.
2. Demand: 100 of those 358 requests are drawn uniformly without replacement with a fixed seed, so the geographic spread and the time of day inside the window follow the source. Origins, destinations and columns are unchanged.
3. Supply: 545 car shifts are online at some point from 18:15 (window start minus a 15-minute lead) to 19:00. Each one is kept with probability `SUPPLY_SCALE` × 100 / 358, with `SUPPLY_SCALE = 0.6` (0.76 cars per request in the sample, against 1.52 in the source). Kept shifts are clipped so they go online no earlier than 18:15 and offline no later than 20:00 (window end plus a 1-hour buffer for trips to finish).
4. Extra columns are passed through unchanged, as in the 10k scenario.

## Stats
- Requests: 100. REGULAR 96, SUV 3, LUXURY 1.
- Supply: 76 car shifts.
- Full figures are in build_stats.json.

## Reference run

settings.toml is the same as the 10k scenario: `greedy_nearest_eta`, `straight_line` at 8 m/s, assignment timeout 300 s, pickup timeout 1200 s, dwells 30/15 s, pooling off, reassignment on, `timezone = "America/Chicago"`.

| routing | requests_completed | requests_canceled | median_wait_seconds | p90_wait_seconds | fleet_utilization | wall time |
|---|---|---|---|---|---|---|
| straight_line | 100 | 0 | 254 | 480 | 0.222032 | about 0.1 s |
| osrm (Austin, no candidate pruning, no cache) — stale, measured on the old 143-shift supply, not rerun | 99 | 1 | 200 | 458 | 0.090035 | about 17 s |

## Supply scale

At the source ratio (`SUPPLY_SCALE = 1.0`, 143 shifts) cars were only about 12% busy. A straight-line sweep of `build.py --supply-scale` on this benchmark (same seed; requests.csv is identical at every scale):

| scale | shifts | completed | canceled | median wait s | p90 wait s | fleet_utilization |
|---|---|---|---|---|---|---|
| 1.0 | 143 | 100 | 0 | 124 | 285 | 0.118 |
| 0.8 | 104 | 100 | 0 | 146 | 373 | 0.160 |
| 0.7 | 92 | 100 | 0 | 167 | 377 | 0.181 |
| 0.6 | 76 | 100 | 0 | 254 | 480 | 0.222 |
| 0.5 | 57 | 100 | 0 | 416 | 781 | 0.277 |
| 0.4 | 45 | 84 | 16 | 783 | 1057 | 0.285 |
| 0.3 | 30 | 39 | 61 | 686 | 1163 | 0.211 |

0.6 is used: it roughly doubles utilization and keeps every request served with waits under 10 minutes at p90, with a margin above the cancellation cliff that starts below 0.5. Utilization cannot get much past 0.28 at any scale, because shifts stay online until 20:00 while demand stops at 19:00.

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
