"""Build the ETA training set: one row per real trip, features known at request time.

Target: trip_time_s, the real pickup -> dropoff duration.

Features (all known before the trip starts):
  - pu_zone, do_zone          where (263 taxi zones)
  - hour, dow, minute_of_day  when (traffic follows the clock)
  - is_holiday                Labor Day behaves like a weekend
  - osrm_s, osrm_mi           free-flow road time and distance between the zones
  - weather                   temperature, precipitation, snowfall, wind (hourly)

Deliberately NOT a feature: trip_miles. It's measured after the trip (the driver's actual
route), so using it would leak the answer.

Split by date, never randomly: trips in the same hour share traffic, so a random split
would let the model "see" the test hour's congestion during training.
"""

from datetime import date

import polars as pl

from fleetline_ml.paths import DERIVED_DIR, ETA_DIR, RAW_DIR

SPLITS = {
    "train": (date(2026, 9, 1), date(2026, 9, 21)),
    "val": (date(2026, 9, 22), date(2026, 9, 24)),
    "test": (date(2026, 9, 25), date(2026, 9, 30)),
}
HOLIDAYS = [date(2026, 9, 7)]  # Labor Day
WEATHER_COLS = ["temperature_2m", "precipitation", "snowfall", "wind_speed_10m"]


def build(month: str = "2026-09") -> dict[str, dict[str, int]]:
    trips = pl.scan_parquet(RAW_DIR / f"fhvhv_tripdata_{month}.parquet").select(
        "pickup_datetime", "PULocationID", "DOLocationID", "trip_time", "trip_miles",
        "shared_match_flag",
    )
    osrm = pl.scan_parquet(DERIVED_DIR / "osrm_zone_matrix.parquet")
    weather = pl.scan_parquet(RAW_DIR / "weather_2026-09-01_2026-09-30.parquet")

    base = (
        trips.filter(
            pl.col("PULocationID").is_between(1, 263) & pl.col("DOLocationID").is_between(1, 263),
            pl.col("shared_match_flag") != "Y",  # pooled rides include other riders' stops
            pl.col("trip_time").is_between(60, 3 * 3600),
        )
        .with_columns(
            pu_zone=pl.col("PULocationID").cast(pl.Int16),
            do_zone=pl.col("DOLocationID").cast(pl.Int16),
            day=pl.col("pickup_datetime").dt.date(),
            hour=pl.col("pickup_datetime").dt.hour().cast(pl.Int8),
            dow=(pl.col("pickup_datetime").dt.weekday() - 1).cast(pl.Int8),  # Mon=0
            minute_of_day=(pl.col("pickup_datetime").dt.hour() * 60
                           + pl.col("pickup_datetime").dt.minute()).cast(pl.Int16),
            hour_ts=pl.col("pickup_datetime").dt.truncate("1h"),
            trip_time_s=pl.col("trip_time").cast(pl.Float32),
            mph=pl.col("trip_miles") / (pl.col("trip_time") / 3600),
        )
        .with_columns(is_holiday=pl.col("day").is_in(HOLIDAYS))
        .join(osrm, on=["pu_zone", "do_zone"], how="left")
        .join(weather, on="hour_ts", how="left")
    )

    ETA_DIR.mkdir(parents=True, exist_ok=True)
    report = {}
    keep = ["pu_zone", "do_zone", "hour", "dow", "minute_of_day", "is_holiday",
            "osrm_s", "osrm_mi", *WEATHER_COLS, "trip_time_s", "pickup_datetime"]
    for name, (lo, hi) in SPLITS.items():
        part = base.filter(pl.col("day").is_between(lo, hi))
        if name == "train":
            # Drop physically implausible rows from training only. The test set keeps
            # them, so the reported error isn't flattered by removing hard cases.
            part = part.filter(pl.col("mph").is_between(1, 70))
        df = part.select(keep).collect()
        df.write_parquet(ETA_DIR / f"{name}.parquet")
        report[name] = {"rows": df.height, "days": (hi - lo).days + 1}
    return report
