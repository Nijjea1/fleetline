"""Calibrate the simulator's travel model from real trips.

The simulator moves vehicles in straight lines, so it needs two numbers from data:

1. **Detour factor**: real road miles / straight-line (haversine) miles. Roads are not
   straight, so this is > 1. We take the median over trips, which ignores weird outliers.
2. **Speed by hour of day**: median of trip_miles / trip_time per request hour. Midnight
   traffic and 5 pm traffic are very different.

3. **Busy vehicles by hour**: how many Uber/Lyft cars were between accepting a request
   and dropping off, sampled at :30 past each hour, from the *raw* month file (so trips
   requested the evening before still count). It is a lower bound on the active fleet,
   which lets us size the simulated fleet from data instead of guessing.

Output: data/derived/travel_model_<day>.json, read by the Go world simulator.
"""

import json
from datetime import date, datetime
from pathlib import Path

import numpy as np
import polars as pl

EARTH_RADIUS_MI = 3958.8


def haversine_mi(lat1, lon1, lat2, lon2) -> np.ndarray:
    p1, p2 = np.radians(lat1), np.radians(lat2)
    dphi = p2 - p1
    dlmb = np.radians(np.asarray(lon2) - np.asarray(lon1))
    a = np.sin(dphi / 2) ** 2 + np.cos(p1) * np.cos(p2) * np.sin(dlmb / 2) ** 2
    return 2 * EARTH_RADIUS_MI * np.arcsin(np.sqrt(a))


def busy_vehicles_by_hour(month_file: Path, day: date) -> list[int]:
    d = pl.scan_parquet(month_file).select("request_datetime", "dropoff_datetime")
    exprs = []
    for h in range(24):
        t = datetime(day.year, day.month, day.day, h, 30)
        busy = (pl.col("request_datetime") <= t) & (pl.col("dropoff_datetime") > t)
        exprs.append(busy.sum().alias(str(h)))
    row = d.select(exprs).collect().row(0)
    return [int(x) for x in row]


def calibrate(day_file: Path, month_file: Path, day: date) -> dict:
    df = pl.read_parquet(day_file)
    straight = haversine_mi(df["pu_lat"], df["pu_lon"], df["do_lat"], df["do_lon"])
    df = df.with_columns(straight_mi=pl.Series(straight))

    # Detour factor only makes sense for real cross-town trips; for very short trips the
    # random within-zone sampling dominates the straight-line distance.
    long_trips = df.filter((pl.col("straight_mi") > 1.0) & (pl.col("trip_miles") > 1.0))
    detour = float((long_trips["trip_miles"] / long_trips["straight_mi"]).median())

    speeds = (
        df.filter(pl.col("trip_time_s") > 60)
        .with_columns(
            hour=pl.col("request_ts").dt.hour(),
            mph=pl.col("trip_miles") / (pl.col("trip_time_s") / 3600),
        )
        .group_by("hour")
        .agg(pl.col("mph").median().alias("mph"), pl.len().alias("trips"))
        .sort("hour")
    )
    return {
        "source_day_file": day_file.name,
        "detour_factor": round(detour, 4),
        "trips_used_for_detour": long_trips.height,
        "speed_mph_by_hour": [round(v, 3) for v in speeds["mph"].to_list()],
        "busy_vehicles_by_hour": busy_vehicles_by_hour(month_file, day),
    }


def write(model: dict, dest: Path) -> Path:
    dest.write_text(json.dumps(model, indent=2) + "\n")
    return dest
