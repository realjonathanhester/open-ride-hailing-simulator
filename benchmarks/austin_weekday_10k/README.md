# austin_weekday_10k

10,000 real RideAustin weekday ride requests, sampled to match the average weekday's time of day and car category mix, all moved onto one synthetic day (Wed 2026-01-07, America/Chicago, so midnight is epoch 1767765600). The supply is the real drivers' shifts, cut down to the sample's scale.

## Source and license
- RideAustin "ride-austin-june-6-april-13" (data.world, now offline: data.world retired its Open Data Community on 2026-07-13). The raw `Rides_DataA.csv` and `Rides_DataB.csv` came from the Git LFS mirror in github.com/mauriciogtec/gfen-reproduce (`raw_data/`), via https://media.githubusercontent.com/media/mauriciogtec/gfen-reproduce/master/raw_data/Rides_Data{A,B}.csv
  - sha256 A: 7e8434eae990e1a125a1fc2f49fa4e2e1c4a61015c44904d8b7e8a38e28cf36f
  - sha256 B: cfcac2b35c0a23ecf4ddf051ae5d18ada389291254f1802a024f894282525d86
- License: the Mendeley Data copy of the same dataset (doi:10.17632/wp5gkjbvb9.1, Zuniga-Garcia et al.) is listed as CC0 1.0. A Kaggle derivative (mexwell/rideaustin-data) is also CC0. The original data.world license page can no longer be reached, so it hasn't been checked.

## Method (build.py, seed 20161001)
1. Join A and B on RIDE_ID. Keep trips inside the Austin bounding box (lat 30.0–30.75, lng -98.1 to -97.4) that last between 0 and 4 h. `requested_at` comes from `created_date`. Drivers come from B's `driver_id`; A's `active_driver_id` is an app-session ID, not a driver.
2. Pool: Mon–Fri from 2016-10-01 to 2017-04-13 (service volume was still ramping up before October). Excluded: US federal holidays, 2016-11-25, 2016-12-23 to 2017-01-02, and SXSW (2017-03-10 to 03-19). That leaves 120 weekdays and 646,410 trips, an average of 5,387 trips per weekday.
3. Demand: the 10,000 trips are split across (15-minute bin × requested_car_category) cells by largest remainder of the pool's distribution, then drawn at random inside each cell. Each trip keeps its real origin and destination (3-decimal source precision, about 100 m) and its time of day. Riders request 1 seat.
4. Supply: shifts are built by chaining each driver's trips across the whole timeline, and a gap of more than 1 h starts a new shift. Each shift starts at its first request minus 5 min, at its first pickup location, and ends at its last dropoff. Each sampled trip's real shift is kept with probability 1/(trips in that shift) (Horvitz–Thompson thinning). That keeps the shift count and timing correct at the sample's scale (about 1.86× an average weekday) instead of length-biased. Shifts are mapped onto the synthetic day relative to the sampled trip's source midnight, so they can start before 00:00 or run past 24:00.
5. Extra columns: requests carry `vehicle_category`, `source_requested_at_local` and `source_driver_id`; supply carries `vehicle_categories`, `source_driver_id` and `source_trips_in_shift`. The spec has no type field, and the CSV reader looks columns up by header name, so it ignores these.

## Stats
- Requests: 10,000. REGULAR 9,592, SUV 283, PREMIUM 99, LUXURY 26 (no HONDA trips in the window).
- Supply: 3,377 car shifts from 1,722 distinct drivers (623 SUV-capable with 6 seats, 2,754 with 4 seats).
- Peak: 413 cars online at 18:41 and 287 active trips at 18:06. Online cars per active trip run about 1.3–1.7 through the day. Hourly figures are in build_stats.json and profile.png.

## Reference run

settings.toml: `greedy_nearest_eta`, `straight_line` at 8 m/s, assignment timeout 300 s, pickup timeout 1200 s, dwells 30/15 s, pooling off, reassignment on. Re-run on commit 7a58497 (about 76 s wall time):

| requests_completed | requests_canceled | canceled_max_request_to_pickup | requests_unserved | median_wait_seconds | p90_wait_seconds | mean_detour_ratio | fleet_utilization |
|---|---|---|---|---|---|---|---|
| 9983 | 17 | 17 | 0 | 118 | 420 | 1.000000 | 0.385087 |

## Caveats
- The data only contains completed trips, so unfulfilled and canceled demand isn't there.
- Driver idle time before the first trip and after the last one isn't recorded, so shifts are a lower bound on online time.
- The category meaning of `car_categories_bitmask` (1=REGULAR, 2=SUV, 4=PREMIUM, 16=LUXURY, 32=HONDA) is inferred, not documented.
- The simulator doesn't match requests to cars by category.
- Straight-line routing with Austin coordinates.

## Rebuild and run

From the repo root (outputs are byte-identical; needs pandas, numpy and matplotlib):

```sh
python3 benchmarks/austin_weekday_10k/build.py --raw <dir with Rides_DataA/B.csv> --out benchmarks/austin_weekday_10k
go run ./cmd/rhs run -settings benchmarks/austin_weekday_10k/settings.toml \
  -supply benchmarks/austin_weekday_10k/supply.csv \
  -requests benchmarks/austin_weekday_10k/requests.csv -output out/austin
```
