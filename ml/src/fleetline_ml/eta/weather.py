"""Hourly NYC weather from the Open-Meteo historical archive (free, no API key).

Rain and snow slow traffic and change demand. Weather is a cheap extra feature, and
"does weather help?" is a clean ablation for E5. Location: Central Park.
"""

import httpx
import polars as pl

from fleetline_ml.paths import RAW_DIR

ARCHIVE = "https://archive-api.open-meteo.com/v1/archive"
VARS = ["temperature_2m", "precipitation", "snowfall", "wind_speed_10m"]


def fetch(start: str, end: str) -> pl.DataFrame:
    r = httpx.get(ARCHIVE, timeout=60, params={
        "latitude": 40.7812, "longitude": -73.9665,
        "start_date": start, "end_date": end,
        "hourly": ",".join(VARS), "timezone": "America/New_York",
    })
    r.raise_for_status()
    h = r.json()["hourly"]
    return pl.DataFrame({"hour_ts": h["time"], **{v: h[v] for v in VARS}}).with_columns(
        pl.col("hour_ts").str.to_datetime("%Y-%m-%dT%H:%M"),
        pl.col(VARS).cast(pl.Float32).fill_null(0),
    )


def write(df: pl.DataFrame, start: str, end: str) -> str:
    dest = RAW_DIR / f"weather_{start}_{end}.parquet"
    df.write_parquet(dest)
    return str(dest)
