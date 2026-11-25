import numpy as np
import pytest

from fleetline_matcher.instances import DAY, Instance, build, objective
from fleetline_matcher.solvers import SOLVERS


def tiny():
    # Greedy trap: rider 0 prefers car 0, which is rider 1's only option.
    return Instance(2, 2, np.array([0, 0, 1], np.int32), np.array([0, 1, 0], np.int32), np.array([1.0, 2.0, 3.0]))


@pytest.mark.parametrize("name", SOLVERS)
def test_beats_greedy_trap(name):
    match, _, _ = SOLVERS[name](tiny())
    assert match.tolist() == [1, 0]


def synthetic_city(seed):
    """Clustered points around Manhattan, for CI (no trip data there)."""
    rng = np.random.default_rng(seed)
    centers = np.array([[40.75, -73.99], [40.70, -73.95], [40.80, -73.94]])
    pts = centers[rng.integers(0, 3, 5000)] + rng.normal(0, 0.02, (5000, 2))
    return pts[:2500], pts[2500:]


@pytest.mark.parametrize("seed", range(3))
def test_solvers_agree(seed):
    inst = build(400, seed=seed, points=synthetic_city(seed))
    totals = {name: objective(inst, fn(inst)[0]) for name, fn in SOLVERS.items()}
    ref = totals["scipy_dense"]
    for name, v in totals.items():
        assert abs(v - ref) <= (0.4 if name == "ortools_flow" else 1e-6), (name, v, ref)


@pytest.mark.skipif(not DAY.exists(), reason="needs data/derived (run sim prepare)")
@pytest.mark.parametrize("seed", range(2))
def test_solvers_agree_on_real_geography(seed):
    inst = build(400, seed=seed)
    totals = {name: objective(inst, fn(inst)[0]) for name, fn in SOLVERS.items()}
    ref = totals["scipy_dense"]
    for name, v in totals.items():
        assert abs(v - ref) <= (0.4 if name == "ortools_flow" else 1e-6), (name, v, ref)


def test_roundtrip(tmp_path):
    inst = build(50, seed=1, points=synthetic_city(1))
    inst.save(tmp_path / "i.bin")
    back = Instance.load(tmp_path / "i.bin")
    assert (back.n_rows, back.n_cols) == (inst.n_rows, inst.n_cols)
    assert np.array_equal(back.costs, inst.costs) and np.array_equal(back.cols, inst.cols)
