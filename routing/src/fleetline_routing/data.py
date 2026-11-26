"""Amazon Last Mile Routing Research Challenge (2021) data, cached per route.

Raw files (aws s3 cp --no-sign-request s3://amazon-last-mile-challenges/almrrc2021/...) go
in data/raw/almrrc/{train,eval}/. `prepare` streams the big travel-time JSON once (the
training file is 1.7 GB of nested JSON: too big to json.load) and writes one pickle per
split with, for every route: stop ids (station first), zone per stop, the driver's actual
order, and the stop-to-stop travel-time matrix in seconds (float32, asymmetric).
"""

import itertools
import json
import pickle
from dataclasses import dataclass
from pathlib import Path

import ijson
import numpy as np

REPO = Path(__file__).resolve().parents[3]
RAW = REPO / "data/raw/almrrc"
CACHE = REPO / "data/derived/almrrc"
FILES = {
    "train": ("route_data", "actual_sequences", "travel_times"),
    "eval": ("eval_route_data", "eval_actual_sequences", "eval_travel_times"),
}


@dataclass
class Route:
    id: str
    station: str
    quality: str  # "High" / "Medium" / "Low" (Amazon's rating of the driver's route); "" if not given
    stops: list[str]  # index 0 = the station
    zones: list[str]  # zone per stop; station gets "STATION"; missing zones filled (see _fill_zones)
    actual: list[int]  # driver's order as indices into stops, starting with 0 (station)
    tt: np.ndarray  # travel time seconds, tt[i, j] from stops[i] to stops[j]

    @property
    def n(self) -> int:
        return len(self.stops)


def _fill_zones(stops, zones, tt):
    """~1% of stops have no zone. Give each the zone of the stop it's quickest to reach from
    (the obvious guess a dispatcher would make)."""
    known = [i for i, z in enumerate(zones) if z is not None and i != 0]
    for i, z in enumerate(zones):
        if i != 0 and z is None and known:
            j = min(known, key=lambda k: tt[i, k] + tt[k, i])
            zones[i] = zones[j]
    zones[0] = "STATION"
    return [z if z is not None else "UNKNOWN" for z in zones]


def prepare(split: str) -> Path:
    route_f, seq_f, tt_f = FILES[split]
    routes = json.loads((RAW / split / f"{route_f}.json").read_text())
    seqs = json.loads((RAW / split / f"{seq_f}.json").read_text())
    out = {}
    with open(RAW / split / f"{tt_f}.json", "rb") as f:
        for rid, mat in ijson.kvitems(f, "", use_float=True):
            r = routes[rid]
            station = next(s for s, v in r["stops"].items() if v["type"] == "Station")
            stops = [station] + sorted(s for s in r["stops"] if s != station)
            idx = {s: i for i, s in enumerate(stops)}
            tt = np.zeros((len(stops), len(stops)), np.float32)
            for a, row in mat.items():
                ia = idx[a]
                for b, t in row.items():
                    tt[ia, idx[b]] = t
            zones = []
            for s in stops:
                z = r["stops"][s]["zone_id"]
                zones.append(z if isinstance(z, str) else None)
            order = sorted(seqs[rid]["actual"].items(), key=lambda kv: kv[1])
            actual = [idx[s] for s, _ in order]
            assert actual[0] == 0, f"{rid}: actual sequence doesn't start at the station"
            out[rid] = Route(
                rid, r["station_code"], r.get("route_score", ""), stops, _fill_zones(stops, zones, tt), actual, tt
            )
    CACHE.mkdir(parents=True, exist_ok=True)
    path = CACHE / f"{split}.pkl"
    with open(path, "wb") as f:
        pickle.dump(out, f, protocol=pickle.HIGHEST_PROTOCOL)
    return path


def load(split: str) -> dict[str, Route]:
    with open(CACHE / f"{split}.pkl", "rb") as f:
        return pickle.load(f)


def tour_time(r: Route, order: list[int]) -> float:
    """Seconds to drive station -> stops in `order` -> back to the station."""
    seq = list(order) + [0]
    return float(sum(r.tt[a, b] for a, b in itertools.pairwise(seq)))
