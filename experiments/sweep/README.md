# rhs-sweep: settings grid runner

This tool sits outside the core simulator and uses only `runner.RunFiles`. For each benchmark folder in `-benchmarks` (the replicates, for example one prebuilt folder per seed), it runs every combination of:

- fleet fraction (`-fleet`, default `0.7,1`). 1 means `supply.csv` as built. Other values thin or copy whole shifts with a seeded draw: folder r uses seed `-seed` + r. Copies get `_copyN` added to `supply_id`.
- `reassignment_enabled` (`-reassignment`, default `true,false`)

`-settings` is the base settings file. Every run sets `[matching] max_candidate_cars` from `-max-candidate-cars`. The default is 10, the value recommended for experiment 1. The core default stays 0.

`results.csv` has one row per run with these columns:

- `settings_sha256`: a hash of the effective settings after overrides, plus fleet fraction, seed and input hashes, so every run's hash is unique.
- `simulator_settings_sha256`: the simulator's own settings hash.
- the commit, the benchmark folder, and its `requests_sha256` and `supply_sha256`
- the seed, every arm value, and the shift and request counts
- `completed`, `canceled_after_assignment`, `never_assigned` (timed out unmatched) and `unresolved_at_end`
- median and p90 wait from the simulator's run_summary.csv, and runtime

Each run's settings, scaled supply and outputs are in `run_NNN/`.

Smoke grid on the 100-ride benchmark (4 runs, under 1 s):

    go run ./experiments/sweep/cmd/rhs-sweep -output out/sweep_100

Build seed folders locally (ignored by git, about 35 s for three in parallel):

    for n in 1 2 3; do python3 benchmarks/austin_weekday_10k/build.py --raw <raw dir> --out benchmarks/seeds/austin_weekday_10k_seed$n --seed $n; done

The default seed 20161001 regenerates the shipped benchmark byte for byte.

Experiment 1 over the seed folders:

    go run ./experiments/sweep/cmd/rhs-sweep -settings benchmarks/austin_weekday_10k/settings.toml -benchmarks benchmarks/seeds/austin_weekday_10k_seed1,benchmarks/seeds/austin_weekday_10k_seed2,benchmarks/seeds/austin_weekday_10k_seed3 -output out/experiment_1

Reproduce the reference row in benchmarks/austin_weekday_10k/README.md (9983 completed, 17 canceled, median 118 s, p90 420 s, about 75 s):

    go run ./experiments/sweep/cmd/rhs-sweep -settings benchmarks/austin_weekday_10k/settings.toml -benchmarks benchmarks/austin_weekday_10k -fleet 1 -reassignment true -max-candidate-cars 0 -output out/reference

## Comparison A example (pooling off versus on)

Internal Austin weekday 10k straight-line example: pooling off versus pooling
on, with `max_candidate_cars = 10`, `reassignment_enabled = true`, fleets 70%
and 100%, and the three Experiment-1 seed folders. Settings files and the
exact commands are in [examples/comparison_a/](examples/comparison_a/). Twelve
runs (two settings × two fleets × three seeds); run them only after review.
