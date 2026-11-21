"""Download public NYC TLC data.

We stream to a temporary ".part" file and rename only when the download completes,
so an interrupted download never leaves a truncated file that looks valid.
"""

from pathlib import Path

import httpx

from fleetline_sim.paths import RAW_DIR

TLC_BASE = "https://d37ci6vzurychx.cloudfront.net"


def hvfhv_url(month: str) -> str:
    """URL for one month of High-Volume FHV (Uber/Lyft) trips. month = 'YYYY-MM'."""
    return f"{TLC_BASE}/trip-data/fhvhv_tripdata_{month}.parquet"


TAXI_ZONES_URL = f"{TLC_BASE}/misc/taxi_zones.zip"


def fetch(url: str, dest: Path, *, force: bool = False) -> Path:
    """Download url to dest unless it already exists. Returns dest."""
    if dest.exists() and not force:
        print(f"[skip] {dest.name} already downloaded")
        return dest
    dest.parent.mkdir(parents=True, exist_ok=True)
    tmp = dest.with_suffix(dest.suffix + ".part")
    with httpx.stream("GET", url, follow_redirects=True, timeout=60) as r:
        r.raise_for_status()
        total = int(r.headers.get("content-length", 0))
        done = 0
        with tmp.open("wb") as f:
            for chunk in r.iter_bytes(chunk_size=1 << 20):
                f.write(chunk)
                done += len(chunk)
                if total:
                    print(f"\r[get] {dest.name} {done / total:6.1%}", end="", flush=True)
    print()
    tmp.rename(dest)
    return dest


def download_month(month: str, *, force: bool = False) -> Path:
    return fetch(hvfhv_url(month), RAW_DIR / f"fhvhv_tripdata_{month}.parquet", force=force)


def download_zones(*, force: bool = False) -> Path:
    return fetch(TAXI_ZONES_URL, RAW_DIR / "taxi_zones.zip", force=force)
