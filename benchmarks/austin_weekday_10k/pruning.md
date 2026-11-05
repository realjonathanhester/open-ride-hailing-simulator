# Candidate pruning on austin_weekday_10k

`[matching] max_candidate_cars` keeps the N closest open cars and the N closest busy cars for each match. 0 means no pruning (the default).

Each row is one run of the reference `settings.toml` (`straight_line`, pooling off, reassignment on) with `max_candidate_cars` set to N. All runs used the same box and commit, with `-progress=false`.

| max_candidate_cars | wall time (s) | matcher time (s) | completed | canceled | median wait (s) | p90 wait (s) | mean detour ratio | pooled |
|---|---|---|---|---|---|---|---|---|
| 5 | 50 | 34.7 | 9981 | 19 | 118 | 396 | 1.000 | 0% |
| 10 | 53 | 38.4 | 9983 | 17 | 117 | 408 | 1.000 | 0% |
| 20 | 54 | 40.9 | 9983 | 17 | 115 | 409 | 1.000 | 0% |
| 0 (unbounded, from README) | about 76 | not recorded | 9983 | 17 | 118 | 420 | 1.000 | 0% |

Notes:
- The unbounded row is the reference run in README.md (commit 7a58497). It was not re-run here, so its wall time is not a same-machine comparison.
- Pooling is off in this benchmark, so the pooled share and the detour ratio don't change.
- Match quality is about the same for every N. N = 5 loses 2 trips to pickup timeouts. N = 10 and N = 20 match the unbounded completed and canceled counts.

Recommendation: use `max_candidate_cars = 10` for large straight-line runs. It is the smallest N that matches the unbounded counts, and it is faster than N = 20. The core default stays 0 so results don't change for existing settings files.
