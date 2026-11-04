# ride-hailing-simulator

A deterministic, event-driven ride-hailing simulator with pluggable matching, routing, and I/O.

**Status:** working Go implementation of the [specification](docs/spec.md), with component tests from the [test specification](docs/test_specs.md).

## Why a simulator?

ride-hailing-simulator is a laboratory for fleet management strategies, such as matching, pooling, batching and reassignment, for ride-hailing and autonomous fleets.

### Production experiments are slow and noisy

Ride-hailing is a two-sided marketplace, and that breaks standard experimentation:

- **A/B tests are biased.** Riders in treatment and control compete for the same cars. A matcher that grabs nearby cars for its own riders looks great while quietly hurting the control group.
- **Splitting supply has its own problems.** Giving each arm its own fleet removes that competition, but each half-size fleet behaves differently from the full marketplace, since density drives ETAs and pooling. Riders also see fewer nearby cars, and you need enough supply to split in the first place.
- **Switchback experiments are underpowered.** Alternating the whole market between strategies over time avoids that interference. But each time block becomes a single data point, so detecting small effects can take weeks.
- **Experiments are costly.** Every bad variant costs real riders, real drivers and real money.

### A perfect world, on purpose

The simulator makes strong simplifying assumptions: cars drive exactly as the routing engine specifies, pickups are seamless, nobody takes a wrong turn, and every assignment is accepted. Those assumptions will never hold in reality, and that's the point. A deterministic runtime gives you:

- **Repeatable runs.** The same inputs always produce the same outputs, to the second and the meter.
- **Paired comparisons.** Run two strategies on identical supply and demand, and any difference is caused by the strategy, not by noise.
- **Speed and cost.** A simulated day of a city takes seconds, not a week of a live market.

The results won't match production numbers. But cheap directional results tell you which ideas deserve an expensive experiment, and which don't.

### Exploring worlds that don't exist yet

Extrapolating from production data is risky when the question is about a very different world. Ride-hailing companies and their investors have made big bets on how a marketplace behaves at scales and with features that no data existed for yet. A simulator lets you ask those questions directly:

- **Scaling.** How much do ETAs drop, and how much does utilization grow, as supply and demand double or grow tenfold?
- **Upper bounds for features.** How much headroom is there in reassigning a rider to a better car after assignment ("pickup swap"), in pooling, in batching windows, or in riders walking to better pickup spots?
- **The cost of constraints.** What do seat limits, cancellation timeouts or a no-reassignment policy actually cost in wait time and miles driven?

Knowing the upper bound tells you whether a feature is worth building before anyone builds it.

### A shared benchmark

Matching algorithms are usually evaluated on private data with private tooling. An open, deterministic simulator with plugin readers, writers and matchers gives researchers and engineers a common place to compare ideas and reproduce each other's results.

## Usage

Requires Go 1.22 or newer.

```sh
go run ./cmd/rhs run \
  -settings tests/fixtures/settings/pooling_enabled.toml \
  -supply tests/fixtures/supply/one_car_at_kezar_stadium.csv \
  -requests tests/fixtures/demand/simple_pool.csv \
  -output out/
```

This writes `trips.csv`, `supply_event_log.csv`, `supply_summary.csv`, `run_summary.csv` and `run_metadata.csv` to `out/`. Paths can also be set in the settings file:

```toml
[io]
reader = "csv"
writer = "csv"
requests_path = "requests.csv"
supply_path = "supply.csv"
output_directory = "out"
```

Built-in hooks: `routing = "straight_line"` (haversine distance, constant `average_speed_meters_per_second`) and `matching = "greedy_nearest_eta"` (also registered as `reference_test_matcher`).

Run the component tests with `go test ./...`.

## Benchmarks

Realistic benchmark scenarios live in `benchmarks/<name>/`, specified in the [benchmark specification](docs/benchmark_spec.md). The first is [`austin_weekday_10k`](benchmarks/austin_weekday_10k/README.md): 10,000 RideAustin weekday requests and 3,377 driver shifts.

## Viewing results

`rhs view` serves a local results viewer (dashboard, trips, supply and run comparison) on `127.0.0.1`. See the [tooling specification](docs/tooling_spec.md).

```sh
go run ./cmd/rhs run -settings tests/fixtures/settings/base_default.toml \
  -supply tests/fixtures/supply/one_car_at_kezar_stadium.csv -requests tests/fixtures/demand/simple_pool.csv -output out/a
go run ./cmd/rhs run -settings tests/fixtures/settings/pooling_enabled.toml \
  -supply tests/fixtures/supply/one_car_at_kezar_stadium.csv -requests tests/fixtures/demand/simple_pool.csv -output out/b
GOOGLE_MAPS_API_KEY=... go run ./cmd/rhs view -output out/a -compare out/b
# open http://127.0.0.1:8080/
```

Maps need a Google Maps JavaScript API key from `GOOGLE_MAPS_API_KEY` or a local, git-ignored `viewer.toml`:

```toml
[viewer]
google_maps_api_key = "..."
google_maps_map_id = "..."  # optional: vector maps and advanced markers
display_timezone = "America/Chicago"  # optional: zone for local times (default: the run's timezone, then the browser's)
```

Without a key every page still works and maps show a notice. Flags: `-output` (required), `-compare`, `-port` (default 8080), `-settings` (default `viewer.toml`), `-timezone` (IANA zone for local times).

## Layout

| Path | Contents |
|---|---|
| `cmd/rhs` | Command-line entry point |
| `model` | Shared records and the hook and plugin interfaces |
| `simulator` | Discrete-event core, plan validation and outputs |
| `routing` | Straight-line router |
| `matching` | Greedy nearest-ETA matcher |
| `plugins/csvio`, `plugins/tomlsettings` | Default CSV readers and writer, TOML settings reader |
| `viewer` | `rhs view` server, aggregation and embedded UI (`viewer/ui`) |
| `runner` | Wires settings, hooks, readers and writers into one run |
| `tests` | Component tests and fixtures |
| `benchmarks` | Benchmark scenarios ([spec](docs/benchmark_spec.md)) |

## License

MIT
