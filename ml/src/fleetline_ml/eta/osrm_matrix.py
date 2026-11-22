"""Zone-to-zone free-flow road times from OSRM.

Our trip data only says which of 263 taxi zones a ride started and ended in, so the
routing baseline works at the same resolution: OSRM driving time between each pair of
zone representative points. One /table request returns the whole 263 x 263 matrix.

Requires the OSRM server from ml/osrm/serve.sh on localhost:5001.
"""

import httpx
import numpy as np
import polars as pl

from fleetline_ml.paths import DERIVED_DIR

OSRM_URL = "http://localhost:5001"


def build(url: str = OSRM_URL) -> pl.DataFrame:
    pts = pl.read_parquet(DERIVED_DIR / "zone_points.parquet").sort("zone")
    coords = ";".join(f"{lon:.6f},{lat:.6f}" for lon, lat in zip(pts["lon"], pts["lat"], strict=True))
    r = httpx.get(f"{url}/table/v1/driving/{coords}", params={"annotations": "duration,distance"},
                  timeout=120)
    r.raise_for_status()
    body = r.json()
    if body.get("code") != "Ok":
        raise RuntimeError(f"OSRM error: {body.get('code')} {body.get('message')}")
    dur = np.array(body["durations"], dtype=np.float64)
    dist = np.array(body["distances"], dtype=np.float64)
    zones = pts["zone"].to_numpy()
    n = len(zones)
    out = pl.DataFrame({
        "pu_zone": np.repeat(zones, n).astype(np.int16),
        "do_zone": np.tile(zones, n).astype(np.int16),
        "osrm_s": dur.ravel().astype(np.float32),
        "osrm_mi": (dist.ravel() / 1609.344).astype(np.float32),
    })
    return out


def write(df: pl.DataFrame) -> str:
    dest = DERIVED_DIR / "osrm_zone_matrix.parquet"
    df.write_parquet(dest)
    return str(dest)
