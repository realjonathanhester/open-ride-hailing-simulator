#!/usr/bin/env python3
"""Build the austin_weekday_10k benchmark scenario from raw RideAustin data.

Usage: build.py --raw /workspace/data/rideaustin --out benchmarks/austin_weekday_10k
Raw files: Rides_DataA.csv, Rides_DataB.csv (RideAustin June 2016 - April 2017).
Deterministic: fixed SEED; same raw files -> byte-identical outputs.
"""
import argparse, json, os
import numpy as np, pandas as pd
from pandas.tseries.holiday import USFederalHolidayCalendar

SEED = 20161001
N_TRIPS = 10_000
TZ = "America/Chicago"
WINDOW = ("2016-10-01", "2017-04-13")          # steady-state service period
EXTRA_EXCLUDE = (["2016-11-25"] + [str(d.date()) for d in pd.date_range("2016-12-23", "2017-01-02")]
                 + [str(d.date()) for d in pd.date_range("2017-03-10", "2017-03-19")])  # Black Friday, holiday week, SXSW
BBOX = (30.0, 30.75, -98.1, -97.4)              # lat min/max, lng min/max (Austin metro)
BIN_SECONDS = 900
SHIFT_GAP_SECONDS = 3600                        # a gap > 1 h between dropoff and next request starts a new shift
ONLINE_LEAD_SECONDS = 300                       # online 5 min before the shift's first request
SYNTH_DATE = "2026-01-07"                       # a Wednesday, CST (UTC-6)
SEATS = {"REGULAR": 4, "SUV": 6, "PREMIUM": 4, "LUXURY": 4, "HONDA": 4}
BITS = {1: "REGULAR", 2: "SUV", 4: "PREMIUM", 8: "BIT8", 16: "LUXURY", 32: "HONDA"}


def clean_b(s):
    return s.astype(str).str.replace(r"^b'(.*)'$", r"\1", regex=True)


def load(raw):
    a = pd.read_csv(os.path.join(raw, "Rides_DataA.csv"), usecols=[
        "RIDE_ID", "created_date", "completed_on", "requested_car_category", "start_location_lat",
        "start_location_long", "end_location_lat", "end_location_long"])
    b = pd.read_csv(os.path.join(raw, "Rides_DataB.csv"), usecols=["RIDE_ID", "driver_id", "car_categories_bitmask"])
    d = a.merge(b, on="RIDE_ID", validate="one_to_one")
    d["category"] = clean_b(d.requested_car_category)
    d["req"] = pd.to_datetime(d.created_date, utc=True).dt.tz_convert(TZ)
    d["done"] = pd.to_datetime(d.completed_on, utc=True).dt.tz_convert(TZ)
    la0, la1, lo0, lo1 = BBOX
    ok = (d.start_location_lat.between(la0, la1) & d.end_location_lat.between(la0, la1)
          & d.start_location_long.between(lo0, lo1) & d.end_location_long.between(lo0, lo1)
          & (d.done > d.req) & ((d.done - d.req).dt.total_seconds() < 4 * 3600) & d.driver_id.notna())
    return d[ok].copy()


def shifts(d):
    d = d.sort_values(["driver_id", "req", "RIDE_ID"]).copy()
    prev_done = d.groupby("driver_id").done.shift()
    new = prev_done.isna() | ((d.req - prev_done).dt.total_seconds() > SHIFT_GAP_SECONDS)
    d["shift_id"] = new.cumsum()
    return d


def main():
    p = argparse.ArgumentParser(); p.add_argument("--raw", required=True); p.add_argument("--out", required=True)
    args = p.parse_args()
    rng = np.random.default_rng(SEED)
    d = shifts(load(args.raw))           # shifts are chained on the full timeline (cross midnight)
    d["date"] = d.req.dt.strftime("%Y-%m-%d")
    hol = {str(x.date()) for x in USFederalHolidayCalendar().holidays("2016-01-01", "2017-12-31")}
    pool_mask = ((d.req.dt.dayofweek < 5) & (d.date >= WINDOW[0]) & (d.date <= WINDOW[1])
                 & ~d.date.isin(hol) & ~d.date.isin(EXTRA_EXCLUDE))
    pool = d[pool_mask].copy()
    midnight = pool.req.dt.normalize()
    pool["tod"] = (pool.req - midnight).dt.total_seconds().astype(int)
    pool["bin"] = pool.tod // BIN_SECONDS
    n_days = pool.date.nunique()

    # Allocate N_TRIPS over (15-min bin x category) cells by largest remainder of the average-weekday profile.
    cells = pool.groupby(["bin", "category"]).size()
    quota = cells / cells.sum() * N_TRIPS
    alloc = np.floor(quota).astype(int)
    rem = (quota - alloc).sort_values(ascending=False, kind="mergesort")
    alloc[rem.index[: N_TRIPS - alloc.sum()]] += 1
    picks = []
    for (bn, cat), k in alloc.items():
        if k == 0: continue
        idx = pool.index[(pool.bin == bn) & (pool.category == cat)]
        picks.extend(rng.choice(np.sort(idx), size=k, replace=False))
    s = pool.loc[sorted(picks)].sort_values(["tod", "RIDE_ID"]).copy()

    synth_mid = pd.Timestamp(SYNTH_DATE, tz=TZ)
    base = int(synth_mid.timestamp())
    s["requested_at"] = base + s.tod
    req = pd.DataFrame({
        "request_id": "ra_" + s.RIDE_ID.astype(str),
        "requested_at": s.requested_at,
        "origin_lat": s.start_location_lat.round(6), "origin_lng": s.start_location_long.round(6),
        "destination_lat": s.end_location_lat.round(6), "destination_lng": s.end_location_long.round(6),
        "num_requested_seats": 1,
        "max_request_to_assignment_seconds": "", "max_request_to_pickup_seconds": "",
        "vehicle_category": s.category,
        "source_requested_at_local": s.req.dt.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "source_driver_id": s.driver_id.astype(int),
    })

    # Supply: each sampled trip's real shift is kept with probability 1/(trips in that shift)
    # (Horvitz-Thompson thinning), so shift count and shift-start timing are unbiased at the sample's scale.
    sh = d.groupby("shift_id").agg(driver_id=("driver_id", "first"), n=("RIDE_ID", "size"),
                                   t0=("req", "min"), t1=("done", "max"), bitmask=("car_categories_bitmask", "max"))
    first = d.sort_values("req").groupby("shift_id").first()
    sh["lat"], sh["lng"] = first.start_location_lat, first.start_location_long
    cand = s.shift_id.values
    keep = rng.random(len(cand)) < 1.0 / sh.loc[cand, "n"].values
    kept = sorted(set(cand[keep]))
    k = sh.loc[kept].copy()
    # remap onto the synthetic day relative to the sampled trip's source day midnight
    anchor = s[s.shift_id.isin(kept)].groupby("shift_id").req.first().dt.normalize()
    k["online_at"] = base + ((k.t0 - anchor).dt.total_seconds().astype(int) - ONLINE_LEAD_SECONDS)
    k["offline_at"] = base + (k.t1 - anchor).dt.total_seconds().astype(int)
    def cats(m):
        m = int(m) if pd.notna(m) else 1
        return "|".join(v for b_, v in BITS.items() if m & b_) or "REGULAR"
    k["cats"] = k.bitmask.map(cats)
    sup = pd.DataFrame({
        "supply_id": ["ra_driver_%d_%s" % (int(r.driver_id), anchor[i].strftime("%Y%m%d")) + ("_s%d" % i) for i, r in k.iterrows()],
        "online_at": k.online_at, "online_lat": k.lat.round(6), "online_lng": k.lng.round(6),
        "offline_at": k.offline_at,
        "num_available_seats": k.cats.map(lambda c: 6 if "SUV" in c else 4),
        "vehicle_categories": k.cats,
        "source_driver_id": k.driver_id.astype(int),
        "source_trips_in_shift": k.n,
    }).sort_values(["online_at", "supply_id"])

    os.makedirs(args.out, exist_ok=True)
    req.to_csv(os.path.join(args.out, "requests.csv"), index=False)
    sup.to_csv(os.path.join(args.out, "supply.csv"), index=False)

    # stats
    real_day = pool.groupby("date").agg(trips=("RIDE_ID", "size"), shifts=("shift_id", "nunique"))
    hr = (s.tod // 3600)
    stats = {
        "seed": SEED, "pool_weekdays": int(n_days), "pool_trips": int(len(pool)),
        "avg_weekday_trips": float(real_day.trips.mean()), "avg_weekday_shifts": float(real_day.shifts.mean()),
        "scale_vs_avg_weekday": N_TRIPS / float(real_day.trips.mean()),
        "requests": int(len(req)), "supply": int(len(sup)), "unique_source_drivers": int(sup.source_driver_id.nunique()),
        "requests_by_category": req.vehicle_category.value_counts().to_dict(),
        "supply_by_seats": sup.num_available_seats.value_counts().to_dict(),
        "requests_by_hour": {int(h): int(c) for h, c in hr.value_counts().sort_index().items()},
    }
    # concurrency by minute over the synthetic day
    t = np.arange(base - 6 * 3600, base + 30 * 3600, 60)
    on = ((sup.online_at.values[None, :] <= t[:, None]) & (sup.offline_at.values[None, :] > t[:, None])).sum(1)
    sdone = base + (s.done - s.req.dt.normalize()).dt.total_seconds().astype(int).values
    act = ((s.requested_at.values[None, :] <= t[:, None]) & (sdone[None, :] > t[:, None])).sum(1)
    stats["peak_online_cars"] = int(on.max()); stats["peak_online_at_local"] = str(pd.Timestamp(t[on.argmax()], unit="s", tz="UTC").tz_convert(TZ))
    stats["peak_active_trips"] = int(act.max()); stats["peak_active_at_local"] = str(pd.Timestamp(t[act.argmax()], unit="s", tz="UTC").tz_convert(TZ))
    hours = (t - base) // 3600
    prof = pd.DataFrame({"h": hours, "on": on, "act": act}).query("0 <= h < 24").groupby("h").mean().round(1)
    stats["hourly_mean_online_cars_vs_active_trips"] = {int(h): [r.on, r.act] for h, r in prof.iterrows()}
    with open(os.path.join(args.out, "build_stats.json"), "w") as f:
        json.dump(stats, f, indent=1, default=str)

    # chart
    import matplotlib; matplotlib.use("Agg"); import matplotlib.pyplot as plt
    ct = pd.crosstab(hr, s.category).reindex(range(24), fill_value=0)
    order = [c for c in ["REGULAR", "SUV", "PREMIUM", "LUXURY", "HONDA"] if c in ct]
    fig, ax = plt.subplots(figsize=(11, 5.5))
    bottom = np.zeros(24)
    for c in order:
        ax.bar(ct.index, ct[c], bottom=bottom, label=f"{c} requests", width=0.85); bottom += ct[c].values
    ax.set_xlabel("Hour of day (America/Chicago)"); ax.set_ylabel("Requests per hour")
    ax2 = ax.twinx()
    ax2.plot(prof.index, prof.on, "k-o", ms=3, label="Online cars (mean)")
    ax2.plot(prof.index, prof.act, "--", color="crimson", label="Active trips (mean)")
    ax2.set_ylabel("Concurrent count"); ax2.set_ylim(0, max(prof.on.max(), prof.act.max()) * 1.15)
    h1, l1 = ax.get_legend_handles_labels(); h2, l2 = ax2.get_legend_handles_labels()
    ax.legend(h1 + h2, l1 + l2, loc="upper left", fontsize=8)
    ax.set_xticks(range(24)); ax.set_title(f"austin_weekday_10k: {len(req):,} requests, {len(sup):,} car shifts (RideAustin weekdays, seed {SEED})")
    fig.tight_layout(); fig.savefig(os.path.join(args.out, "profile.png"), dpi=130)
    print(json.dumps(stats, indent=1, default=str))


if __name__ == "__main__":
    main()
