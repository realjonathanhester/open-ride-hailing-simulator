#!/usr/bin/env python3
"""Build austin_weekday_100 from austin_weekday_10k (standard library only, deterministic).

Window: 18:30 to 19:00 America/Chicago on the synthetic day (the busiest 30 minutes for requests).
Requests: 100 drawn without replacement from the 10k requests in the window, using a fixed seed.
Supply: each car shift online at any point from window start minus the lead time to window end is
kept with probability SUPPLY_SCALE * 100 / (requests in window), i.e. SUPPLY_SCALE times the
cars-per-request ratio of the 10k scenario. Kept shifts are clipped to [window start minus lead, window end plus finish buffer].
"""
import argparse, csv, json, random
from pathlib import Path

MIDNIGHT = 1767765600
WINDOW_START = MIDNIGHT + 18 * 3600 + 30 * 60
WINDOW_END = WINDOW_START + 30 * 60
LEAD_SECONDS = 15 * 60
FINISH_BUFFER_SECONDS = 60 * 60
SEED = 20161001
REQUEST_COUNT = 100
# Fraction of the source cars-per-request ratio kept in the sample. 1.0 reproduces the source ratio
# (143 shifts, cars about 12% busy). 0.6 was picked by a straight-line sweep as the least aggressive
# cut that roughly doubles utilization while staying clear of the cancellation cliff below 0.5.
# Requests are drawn before supply, so changing this never changes requests.csv.
SUPPLY_SCALE = 0.6

def read(path):
    with open(path, newline="") as f:
        reader = csv.DictReader(f)
        return reader.fieldnames, list(reader)

def write(path, fields, rows):
    with open(path, "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fields, lineterminator="\n")
        writer.writeheader()
        writer.writerows(rows)

def main():
    here = Path(__file__).resolve().parent
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", default=str(here.parent / "austin_weekday_10k"))
    parser.add_argument("--out", default=str(here))
    parser.add_argument("--supply-scale", type=float, default=SUPPLY_SCALE)
    arguments = parser.parse_args()
    source, out = Path(arguments.source), Path(arguments.out)
    random_generator = random.Random(SEED)

    request_fields, requests = read(source / "requests.csv")
    in_window = [r for r in requests if WINDOW_START <= int(r["requested_at"]) < WINDOW_END]
    sample = random_generator.sample(in_window, REQUEST_COUNT)
    sample.sort(key=lambda r: (int(r["requested_at"]), r["request_id"]))

    supply_fields, supply = read(source / "supply.csv")
    early, late = WINDOW_START - LEAD_SECONDS, WINDOW_END + FINISH_BUFFER_SECONDS
    keep_probability = arguments.supply_scale * REQUEST_COUNT / len(in_window)
    online = [s for s in supply if int(s["online_at"]) < WINDOW_END and int(s["offline_at"]) > early]
    kept = []
    for shift in online:
        if random_generator.random() < keep_probability:
            shift = dict(shift)
            shift["online_at"] = str(max(int(shift["online_at"]), early))
            shift["offline_at"] = str(min(int(shift["offline_at"]), late))
            kept.append(shift)
    kept.sort(key=lambda s: (int(s["online_at"]), s["supply_id"]))

    write(out / "requests.csv", request_fields, sample)
    write(out / "supply.csv", supply_fields, kept)
    stats = {
        "seed": SEED,
        "source": "austin_weekday_10k",
        "window_local": "18:30-19:00 America/Chicago",
        "window_start": WINDOW_START,
        "window_end": WINDOW_END,
        "lead_seconds": LEAD_SECONDS,
        "finish_buffer_seconds": FINISH_BUFFER_SECONDS,
        "source_requests_in_window": len(in_window),
        "source_supply_online_in_window_with_lead": len(online),
        "supply_scale": arguments.supply_scale,
        "keep_probability": keep_probability,
        "requests": len(sample),
        "supply": len(kept),
        "supply_per_request_source": len(online) / len(in_window),
        "supply_per_request": len(kept) / len(sample),
        "requests_by_category": {},
    }
    for r in sample:
        category = r["vehicle_category"]
        stats["requests_by_category"][category] = stats["requests_by_category"].get(category, 0) + 1
    (out / "build_stats.json").write_text(json.dumps(stats, indent=1, sort_keys=False) + "\n")

if __name__ == "__main__":
    main()
