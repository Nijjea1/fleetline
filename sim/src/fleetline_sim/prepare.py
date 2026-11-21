"""Build a replay day: one cleaned, geocoded parquet file per day of demand.

Input:  raw HVFHV month + taxi zone shapefile
Output: data/derived/day_<YYYY-MM-DD>_seed<S>.parquet, one row per ride request, sorted by
        request time, with sampled pickup/dropoff coordinates and H3 cells.

Every cleaning rule is counted and printed, so the README can state exactly what was dropped.
"""

from datetime import date
from pathlib import Path

import h3
import numpy as np
import polars as pl

from fleetline_sim.paths import DERIVED_DIR, RAW_DIR
from fleetline_sim.zones import load_zones, sample_points_in_polygon, to_lonlat

# H3 resolution 8: hexagons ~0.74 km² (edge ~0.53 km). Fine enough for "nearby driver"
# lookups; Kafka partitioning uses a coarser parent cell (see core/).
H3_RES = 8

COMPANY = {"HV0003": "uber", "HV0005": "lyft", "HV0004": "via", "HV0002": "juno"}


def _clean(df: pl.DataFrame, valid_zones: set[int]) -> tuple[pl.DataFrame, dict[str, int]]:
    """Apply cleaning rules in order, recording how many rows each rule removed."""
    rules: list[tuple[str, pl.Expr]] = [
        (
            "unknown pickup/dropoff zone",
            pl.col("PULocationID").is_in(valid_zones) & pl.col("DOLocationID").is_in(valid_zones),
        ),
        ("driver on scene before request (negative wait)", pl.col("real_wait_s") >= 0),
        ("wait over 1 hour (likely scheduled ride)", pl.col("real_wait_s") <= 3600),
        ("non-positive trip time", pl.col("trip_time") > 0),
        ("dropoff before pickup", pl.col("dropoff_datetime") > pl.col("pickup_datetime")),
    ]
    report: dict[str, int] = {}
    for name, keep in rules:
        before = df.height
        df = df.filter(keep)
        report[name] = before - df.height
    return df, report


def build_day(month_file: Path, zones_zip: Path, day: date, seed: int) -> Path:
    zones = load_zones(zones_zip)
    geom_by_id = dict(zip(zones["LocationID"], zones.geometry, strict=True))

    df = (
        pl.scan_parquet(month_file)
        .filter(pl.col("request_datetime").dt.date() == day)
        .with_columns(
            real_wait_s=(
                pl.col("on_scene_datetime") - pl.col("request_datetime")
            ).dt.total_seconds()
        )
        .collect()
    )
    raw_rows = df.height
    df, report = _clean(df, set(geom_by_id))

    # Stable order before sampling => same seed gives the same coordinates every run.
    df = df.sort(["request_datetime", "PULocationID", "DOLocationID", "trip_time"])
    df = df.with_row_index("trip_id")

    rng = np.random.default_rng(seed)
    coords = {}
    for end in ("PU", "DO"):
        ids = df[f"{end}LocationID"].to_numpy()
        x = np.empty(len(ids))
        y = np.empty(len(ids))
        for zone_id in np.unique(ids):  # sorted => deterministic rng consumption
            idx = np.flatnonzero(ids == zone_id)
            x[idx], y[idx] = sample_points_in_polygon(geom_by_id[zone_id], len(idx), rng)
        coords[end] = to_lonlat(x, y)

    pu_lon, pu_lat = coords["PU"]
    do_lon, do_lat = coords["DO"]
    pu_h3 = [h3.latlng_to_cell(a, o, H3_RES) for a, o in zip(pu_lat, pu_lon, strict=True)]

    out = df.select(
        "trip_id",
        pl.col("hvfhs_license_num").replace_strict(COMPANY, default="other").alias("company"),
        pl.col("request_datetime").alias("request_ts"),
        pl.col("on_scene_datetime").alias("real_on_scene_ts"),
        pl.col("pickup_datetime").alias("real_pickup_ts"),
        pl.col("dropoff_datetime").alias("real_dropoff_ts"),
        "real_wait_s",
        pl.col("PULocationID").alias("pu_zone"),
        pl.col("DOLocationID").alias("do_zone"),
        "trip_miles",
        pl.col("trip_time").alias("trip_time_s"),
        (pl.col("shared_request_flag") == "Y").alias("shared_request"),
        (pl.col("wav_request_flag") == "Y").alias("wav_request"),
    ).with_columns(
        pu_lon=pl.Series(pu_lon),
        pu_lat=pl.Series(pu_lat),
        do_lon=pl.Series(do_lon),
        do_lat=pl.Series(do_lat),
        pu_h3=pl.Series(pu_h3),
    )

    DERIVED_DIR.mkdir(parents=True, exist_ok=True)
    dest = DERIVED_DIR / f"day_{day.isoformat()}_seed{seed}.parquet"
    out.write_parquet(dest)

    print(f"raw requests on {day}: {raw_rows:,}")
    for name, n in report.items():
        print(f"  dropped {n:>7,}  {name}")
    print(f"kept {out.height:,} ({out.height / raw_rows:.2%}) -> {dest.name}")
    return dest


def month_file_for(day: date) -> Path:
    return RAW_DIR / f"fhvhv_tripdata_{day:%Y-%m}.parquet"
