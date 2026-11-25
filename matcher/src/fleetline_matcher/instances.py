"""Assignment instances built from real NYC geography.

Riders stand at real pickup points from the 8 AM hour; cars sit at real drop-off points
(where cars actually end up). Like the dispatcher, each rider links to its K nearest cars
within reach of the 10-minute pickup cap, with cost = estimated pickup seconds
(straight line x detour factor / hour-of-day speed, from the calibrated travel model).

File format (little-endian), read by core/cmd/assignbench:
    int32 n_rows, int32 n_cols, int32 n_edges
    int32 rows[n_edges], int32 cols[n_edges], float64 costs[n_edges]
"""

import json
from dataclasses import dataclass
from pathlib import Path

import numpy as np
import pyarrow.compute as pc
import pyarrow.parquet as pq
from scipy.spatial import cKDTree

REPO = Path(__file__).resolve().parents[3]
DAY = REPO / "data/derived/day_2026-09-16_seed7.parquet"
MODEL = REPO / "data/derived/travel_model_2026-09-16.json"
MI_PER_DEG_LAT = 69.0
UNMATCHED = 1e6  # cost of leaving a rider unmatched: above any possible total-time change


@dataclass
class Instance:
    n_rows: int
    n_cols: int
    rows: np.ndarray  # int32
    cols: np.ndarray  # int32
    costs: np.ndarray  # float64

    @property
    def n_edges(self) -> int:
        return len(self.costs)

    def save(self, path: Path) -> None:
        with open(path, "wb") as f:
            np.array([self.n_rows, self.n_cols, self.n_edges], dtype="<i4").tofile(f)
            self.rows.astype("<i4").tofile(f)
            self.cols.astype("<i4").tofile(f)
            self.costs.astype("<f8").tofile(f)

    @classmethod
    def load(cls, path: Path) -> "Instance":
        raw = Path(path).read_bytes()
        n_rows, n_cols, n_edges = np.frombuffer(raw, "<i4", 3)
        o = 12
        rows = np.frombuffer(raw, "<i4", n_edges, o)
        o += 4 * n_edges
        cols = np.frombuffer(raw, "<i4", n_edges, o)
        o += 4 * n_edges
        costs = np.frombuffer(raw, "<f8", n_edges, o)
        return cls(int(n_rows), int(n_cols), rows, cols, costs)


def _points(hour: int = 8):
    t = pq.read_table(DAY, columns=["request_ts", "pu_lat", "pu_lon", "do_lat", "do_lon"])
    h = pc.hour(t["request_ts"]).to_numpy()
    col = {c: t[c].to_numpy() for c in ("pu_lat", "pu_lon", "do_lat", "do_lon")}
    now, around = h == hour, (h >= hour - 1) & (h <= hour)
    pickups = np.column_stack([col["pu_lat"][now], col["pu_lon"][now]])
    dropoffs = np.column_stack([col["do_lat"][around], col["do_lon"][around]])
    return pickups, dropoffs


def build(
    n_riders: int,
    cars_per_rider: float = 2.0,
    k: int = 8,
    cap_s: float = 600,
    hour: int = 8,
    seed: int = 0,
    points: tuple[np.ndarray, np.ndarray] | None = None,
) -> Instance:
    """n_riders riders and n_riders * cars_per_rider cars, sampled from real points.

    Bigger instances sample more points from the same city, so the city gets denser, as a
    longer batching window or a bigger fleet would make it. `points` = (pickups, dropoffs)
    as [lat, lon] arrays overrides the trip data (tests and CI have no data/).
    """
    if MODEL.exists():
        model = json.loads(MODEL.read_text())
        speed_mph, detour = model["speed_mph_by_hour"][hour], model["detour_factor"]
    else:
        speed_mph, detour = 13.0, 1.33  # typical calibrated values, for runs without data/
    speed_mps = speed_mph * 1609.344 / 3600
    pickups, dropoffs = points if points is not None else _points(hour)
    rng = np.random.default_rng(seed)
    n_cars = round(n_riders * cars_per_rider)
    riders = pickups[rng.choice(len(pickups), n_riders, replace=n_riders > len(pickups))]
    cars = dropoffs[rng.choice(len(dropoffs), n_cars, replace=n_cars > len(dropoffs))]
    # A few meters of jitter so sampled-with-replacement points aren't exact duplicates.
    cars = cars + rng.normal(0, 1e-4, cars.shape)

    # Local flat projection (miles) is accurate to <1% across NYC.
    coslat = np.cos(np.radians(40.73))

    def xy(p):
        return np.column_stack([p[:, 1] * coslat * MI_PER_DEG_LAT, p[:, 0] * MI_PER_DEG_LAT])

    reach_mi = cap_s * speed_mps / detour / 1609.344
    kk = min(k, n_cars)
    dist, idx = cKDTree(xy(cars)).query(xy(riders), k=kk, distance_upper_bound=reach_mi)
    dist, idx = dist.reshape(n_riders, kk), idx.reshape(n_riders, kk)
    ok = np.isfinite(dist)
    rows = np.repeat(np.arange(n_riders, dtype=np.int32), kk)[ok.ravel()]
    cols = idx[ok].astype(np.int32)
    costs = dist[ok] * 1609.344 * detour / speed_mps
    return Instance(n_riders, n_cars, rows, cols, costs.astype(np.float64))


def objective(inst: Instance, match: np.ndarray, unmatched: float = UNMATCHED) -> float:
    """Total cost of match (row -> col or -1); raises if it isn't a valid assignment."""
    cost = {}
    for r, c, w in zip(inst.rows.tolist(), inst.cols.tolist(), inst.costs.tolist()):
        cost[(r, c)] = min(w, cost.get((r, c), np.inf))
    used = match[match >= 0]
    if len(used) != len(set(used.tolist())):
        raise ValueError("a car is assigned twice")
    total = 0.0
    for r, c in enumerate(match.tolist()):
        if c < 0:
            total += unmatched
        elif (r, c) not in cost:
            raise ValueError(f"row {r} assigned to non-edge column {c}")
        else:
            total += cost[(r, c)]
    return total
