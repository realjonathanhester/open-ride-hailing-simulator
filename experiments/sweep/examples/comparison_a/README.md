# Comparison A: pooling off versus pooling on

Internal example for the Austin weekday 10k straight-line grid. Pooling off
versus pooling on is the comparison arm. Built-in pooling has unlimited
detours; neither settings file changes that.

Fixed arms (do not improvise others for this comparison):

- `[matching] max_candidate_cars = 10` (via `-max-candidate-cars 10`)
- `reassignment_enabled = true` (via `-reassignment true`)
- fleet fractions 70% and 100% (via `-fleet 0.7,1`)
- the three Experiment-1 seed folders (build locally; gitignored)

Build the seed folders once if they are not already present:

    for n in 1 2 3; do
      python3 benchmarks/austin_weekday_10k/build.py \
        --raw <raw dir> \
        --out benchmarks/seeds/austin_weekday_10k_seed$n \
        --seed $n
    done

Run the twelve cells as two six-run sweeps (three seeds × two fleets each),
one per pooling setting:

    go run ./experiments/sweep/cmd/rhs-sweep \
      -settings experiments/sweep/examples/comparison_a/settings_pooling_off.toml \
      -benchmarks benchmarks/seeds/austin_weekday_10k_seed1,benchmarks/seeds/austin_weekday_10k_seed2,benchmarks/seeds/austin_weekday_10k_seed3 \
      -fleet 0.7,1 \
      -reassignment true \
      -max-candidate-cars 10 \
      -output out/comparison_a_pooling_off

    go run ./experiments/sweep/cmd/rhs-sweep \
      -settings experiments/sweep/examples/comparison_a/settings_pooling_on.toml \
      -benchmarks benchmarks/seeds/austin_weekday_10k_seed1,benchmarks/seeds/austin_weekday_10k_seed2,benchmarks/seeds/austin_weekday_10k_seed3 \
      -fleet 0.7,1 \
      -reassignment true \
      -max-candidate-cars 10 \
      -output out/comparison_a_pooling_on

Each command writes `results.csv` and per-run folders under its `-output`
directory. Do not publish Comparison A findings from this example; it is for
internal runs after review.
