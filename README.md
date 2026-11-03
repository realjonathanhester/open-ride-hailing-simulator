# open-ride-hailing-simulator

A deterministic, event-driven ride-hailing simulator with pluggable matching, routing, and I/O.

**Status:** working Go implementation of the [specification](docs/spec.md), with component tests from the [test specification](docs/test_specs.md).

## Usage

Requires Go 1.22 or newer.

```sh
go run ./cmd/orhs \
  -settings tests/fixtures/settings/pooling_enabled.toml \
  -supply tests/fixtures/supply/one_car_at_kezar_stadium.csv \
  -requests tests/fixtures/demand/simple_pool.csv \
  -output out/
```

This writes `trips.csv`, `supply_event_log.csv`, `supply_summary.csv` and `run_summary.csv` to `out/`. Paths can also be set in the settings file:

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

## Layout

| Path | Contents |
|---|---|
| `cmd/orhs` | Command-line entry point |
| `model` | Shared records and the hook and plugin interfaces |
| `simulator` | Discrete-event core, plan validation and outputs |
| `routing` | Straight-line router |
| `matching` | Greedy nearest-ETA matcher |
| `plugins/csvio`, `plugins/tomlsettings` | Default CSV readers and writer, TOML settings reader |
| `runner` | Wires settings, hooks, readers and writers into one run |
| `tests` | Component tests and fixtures |

## License

MIT
