import itertools

import numpy as np
import pytest

from fleetline_routing import data, solve


def synthetic(n_zones=4, per_zone=6, seed=0):
    """Stops in tight clusters (zones) on a line; travel time = distance."""
    rng = np.random.default_rng(seed)
    pts = [np.zeros(2)]
    zones = ["STATION"]
    for z in range(n_zones):
        center = np.array([10.0 * (z + 1), 0.0])
        for _ in range(per_zone):
            pts.append(center + rng.normal(0, 1.0, 2))
            zones.append(f"A-{z // 2}.{z % 2}A")
    pts = np.array(pts)
    tt = np.linalg.norm(pts[:, None] - pts[None], axis=-1).astype(np.float32) * 60
    n = len(pts)
    return data.Route("r", "S", "High", [f"s{i}" for i in range(n)], zones, list(range(n)), tt)


def valid(order, n):
    return order[0] == 0 and sorted(order) == list(range(n))


def test_shortest_is_a_valid_tour_and_optimal_on_tiny():
    r = synthetic(n_zones=2, per_zone=3)
    order = solve.shortest(r, 0.5)
    assert valid(order, r.n)
    best = min(data.tour_time(r, [0, *p]) for p in itertools.permutations(range(1, r.n)))
    assert data.tour_time(r, order) == pytest.approx(best, rel=1e-4)


def test_huge_zone_penalty_keeps_zones_contiguous():
    r = synthetic()
    # Scramble zone labels so pure distance would interleave them.
    r.zones = ["STATION"] + [f"A-0.{i % 3}B" for i in range(1, r.n)]
    order = solve.zones(r, solve.ZonePenalty(1e5, 1e5, 1e5), 0.5)
    assert valid(order, r.n)
    seen = [r.zones[i] for i in order[1:]]
    runs = [k for k, _ in itertools.groupby(seen)]
    assert len(runs) == len(set(seen)), f"zones visited in more than one run: {runs}"


def test_penalty_grades():
    r = synthetic(n_zones=1, per_zone=1)
    r.stops, r.zones = ["S", "a", "b", "c", "d"], ["STATION", "A-1.1A", "A-1.1B", "A-1.2A", "B-3.1A"]
    r.tt = np.ones((5, 5), np.float32)
    pen = solve.penalty_matrix(r, solve.ZonePenalty(1, 10, 100))
    assert pen[1, 1] == 0 and pen[1, 2] == 1 and pen[1, 3] == 10 and pen[1, 4] == 100
    assert pen[0, 4] == 0 and pen[4, 0] == 0


def test_official_score_is_zero_for_the_drivers_own_order():
    official = pytest.importorskip("fleetline_routing.official")
    r = synthetic(n_zones=2, per_zone=3)
    try:
        assert official.route_score(r, r.actual) == pytest.approx(0.0)
        # Note: the official metric scores an exactly *reversed* order 0 too (sequence
        # deviation only counts jumps between non-adjacent stops), so use a shuffle.
        shuffled = [0, 3, 1, 5, 2, 6, 4]
        assert official.route_score(r, shuffled) > 0
    except OSError as e:  # scorer is downloaded on first use; no network -> skip
        pytest.skip(f"official scorer unavailable: {e}")
