# Comparison B: greedy pooling versus transfer-enabled greedy

Internal example for the Austin weekday 10k straight-line grid. Control is the
Comparison-A built-in `greedy_nearest_eta` with pooling on. Treatment is the
same ordinary greedy behavior plus an optional single transfer
(`transfermatcher`) when a candidate passes `max_rider_path_ratio = 1.25` and
has a strictly earlier pickup ETA. Treatment enables handoffs with a fixed
`max_handoff_wait_seconds = 300`.

**Do not run these sweeps until Technical Program Manager review of the
matcher PR.** This README documents the locked commands only.

Fixed arms (do not improvise others for this comparison):

- `[matching] max_candidate_cars = 10` (via `-max-candidate-cars 10`)
- `reassignment_enabled = true` (via `-reassignment true`)
- fleet fractions 70% and 100% (via `-fleet 0.7,1`)
- build seeds 1/2/3 with supply seeds 20161001/2/3
- treatment records `transfers_enabled = true`, `max_rider_path_ratio = 1.25` and
  `max_insertion_evaluations = 5000` (transfer/provisional/onward scoring only) in run metadata

Build the seed folders once if they are not already present (same as
Comparison A):

    for n in 1 2 3; do
      python3 benchmarks/austin_weekday_10k/build.py \
        --raw <raw dir> \
        --out benchmarks/seeds/austin_weekday_10k_seed$n \
        --seed $n
    done

After review, run the twelve cells as two six-run sweeps (three seeds × two
fleets each), one per arm. Use supply base seed 20161001 so folders get
20161001/2/3:

    go run ./experiments/sweep/cmd/rhs-sweep \
      -settings experiments/sweep/examples/comparison_b/settings_control.toml \
      -benchmarks benchmarks/seeds/austin_weekday_10k_seed1,benchmarks/seeds/austin_weekday_10k_seed2,benchmarks/seeds/austin_weekday_10k_seed3 \
      -fleet 0.7,1 \
      -reassignment true \
      -max-candidate-cars 10 \
      -seed 20161001 \
      -output out/comparison_b_control

    go run ./experiments/sweep/cmd/rhs-sweep \
      -settings experiments/sweep/examples/comparison_b/settings_treatment.toml \
      -benchmarks benchmarks/seeds/austin_weekday_10k_seed1,benchmarks/seeds/austin_weekday_10k_seed2,benchmarks/seeds/austin_weekday_10k_seed3 \
      -fleet 0.7,1 \
      -reassignment true \
      -max-candidate-cars 10 \
      -seed 20161001 \
      -output out/comparison_b_treatment

Each command writes `results.csv` and per-run folders under its `-output`
directory. Do not publish Comparison B findings from this example; it is for
internal runs after review.
