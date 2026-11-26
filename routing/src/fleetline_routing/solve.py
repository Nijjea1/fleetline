"""Stop sequencers. Each returns the visiting order as indices into route.stops, starting
with 0 (the station); the route implicitly returns to the station.

- shortest: the fastest tour (asymmetric TSP) by the challenge's own travel times.
- zones:    the fastest tour after adding a penalty to every edge that leaves a delivery
            zone, graded by how far apart the zone ids are. Drivers finish an area before
            moving on; this asks the solver to do the same, then minimizes time within that.
Both use OR-Tools routing (cheapest-arc start + guided local search) with a time limit.
"""

from dataclasses import dataclass

import numpy as np
from ortools.constraint_solver import pywrapcp, routing_enums_pb2

from .data import Route

SCALE = 10  # costs in deciseconds (OR-Tools needs integers)


@dataclass(frozen=True)
class ZonePenalty:
    """Seconds added to an edge between zones. Zone ids look like 'A-2.2C': region 'A',
    area 'A-2', block 'A-2.2', zone 'A-2.2C'. Leaving a zone for a sibling in the same block
    costs `block`, for another block in the same area `area`, otherwise `far`."""

    block: float
    area: float
    far: float


def _parts(z: str):
    if z in ("STATION", "UNKNOWN") or "-" not in z or "." not in z:
        return (z, z, z)
    area = z.split(".")[0]  # 'A-2'
    block = z[:-1]  # 'A-2.2'
    return (area, block, z)


def penalty_matrix(r: Route, p: ZonePenalty) -> np.ndarray:
    parts = [_parts(z) for z in r.zones]
    area = np.array([a for a, _, _ in parts])
    block = np.array([b for _, b, _ in parts])
    zone = np.array([z for _, _, z in parts])
    same_zone = zone[:, None] == zone[None, :]
    same_block = block[:, None] == block[None, :]
    same_area = area[:, None] == area[None, :]
    pen = np.where(same_zone, 0.0, np.where(same_block, p.block, np.where(same_area, p.area, p.far)))
    pen[0, :] = 0  # leaving or returning to the station is never penalized
    pen[:, 0] = 0
    return pen


def solve_tour(cost: np.ndarray, time_limit_s: float) -> list[int]:
    """Min-cost tour from node 0 over an asymmetric cost matrix (seconds)."""
    n = len(cost)
    if n <= 2:
        return list(range(n))
    mgr = pywrapcp.RoutingIndexManager(n, 1, 0)
    model = pywrapcp.RoutingModel(mgr)
    mat = np.round(cost * SCALE).astype(np.int64).tolist()
    cb = model.RegisterTransitMatrix(mat)
    model.SetArcCostEvaluatorOfAllVehicles(cb)
    params = pywrapcp.DefaultRoutingSearchParameters()
    params.first_solution_strategy = routing_enums_pb2.FirstSolutionStrategy.PATH_CHEAPEST_ARC
    params.local_search_metaheuristic = routing_enums_pb2.LocalSearchMetaheuristic.GUIDED_LOCAL_SEARCH
    params.time_limit.FromMilliseconds(int(time_limit_s * 1000))
    sol = model.SolveWithParameters(params)
    if sol is None:
        raise RuntimeError("OR-Tools found no tour")
    order, i = [], model.Start(0)
    while not model.IsEnd(i):
        order.append(mgr.IndexToNode(i))
        i = sol.Value(model.NextVar(i))
    return order


def shortest(r: Route, time_limit_s: float = 1.0) -> list[int]:
    return solve_tour(r.tt.astype(np.float64), time_limit_s)


def zones(r: Route, p: ZonePenalty, time_limit_s: float = 1.0) -> list[int]:
    return solve_tour(r.tt.astype(np.float64) + penalty_matrix(r, p), time_limit_s)
