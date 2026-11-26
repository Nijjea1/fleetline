"""GPU auction (matcher/cuda) against the exact CPU solvers. Skipped unless it was built and
a CUDA GPU is present (CI has neither)."""

import numpy as np
import pytest
from test_solvers import synthetic_city

from fleetline_matcher.instances import Instance, build, objective
from fleetline_matcher.solvers import SOLVERS

pytestmark = pytest.mark.skipif("cuda_auction" not in SOLVERS, reason="CUDA solver not built (matcher/cuda/build.sh)")


def cuda(inst):
    return SOLVERS["cuda_auction"](inst)[0]


def exact(inst):
    return objective(inst, SOLVERS["scipy_sparse"](inst)[0])


def inst(n_rows, n_cols, edges):
    r, c, w = zip(*edges) if edges else ((), (), ())
    return Instance(n_rows, n_cols, np.array(r, np.int32), np.array(c, np.int32), np.array(w, np.float64))


def test_empty():
    assert cuda(inst(0, 0, [])).tolist() == []


def test_single_rider():
    assert cuda(inst(1, 1, [(0, 0, 4.0)])).tolist() == [0]
    assert cuda(inst(1, 3, [(0, 0, 4.0), (0, 2, 1.5)])).tolist() == [2]


def test_unreachable_rider_stays_unmatched():
    assert cuda(inst(2, 1, [(0, 0, 4.0)])).tolist() == [0, -1]
    assert cuda(inst(2, 2, [])).tolist() == [-1, -1]


def test_greedy_trap():
    # Rider 0 prefers car 0, which is rider 1's only option.
    assert cuda(inst(2, 2, [(0, 0, 1), (0, 1, 2), (1, 0, 3)])).tolist() == [1, 0]


def test_long_augmenting_path():
    # Rider 2 can only use car 0, which forces riders 0 and 1 to shift along.
    edges = [(0, 0, 1), (0, 1, 5), (1, 1, 1), (1, 2, 5), (2, 0, 2)]
    assert cuda(inst(3, 3, edges)).tolist() == [1, 2, 0]


def test_duplicate_edges_use_the_cheapest():
    assert cuda(inst(1, 1, [(0, 0, 9.0), (0, 0, 2.0)])).tolist() == [0]


@pytest.mark.parametrize("seed", range(200))
def test_random_small_dense(seed):
    # Small integer costs make ties common, which is where auction bugs hide.
    rng = np.random.default_rng(seed)
    n, m = rng.integers(1, 9, 2)
    mask = rng.random((n, m)) < 0.6
    r, c = np.nonzero(mask)
    i = Instance(int(n), int(m), r.astype(np.int32), c.astype(np.int32), rng.integers(0, 10, len(r)).astype(float))
    assert abs(objective(i, cuda(i)) - exact(i)) <= 1e-6


@pytest.mark.parametrize("seed", range(20))
def test_random_float_costs(seed):
    rng = np.random.default_rng(1000 + seed)
    n, m = int(rng.integers(5, 60)), int(rng.integers(5, 120))
    k = int(rng.integers(1, 6))
    r = np.repeat(np.arange(n, dtype=np.int32), k)
    c = rng.integers(0, m, n * k).astype(np.int32)
    i = Instance(n, m, r, c, rng.random(n * k) * 600)
    assert abs(objective(i, cuda(i)) - exact(i)) <= 1e-3 * n


@pytest.mark.parametrize("seed", range(3))
@pytest.mark.parametrize("n", [400, 5000])
def test_synthetic_city(seed, n):
    i = build(n, seed=seed, points=synthetic_city(seed))
    assert abs(objective(i, cuda(i)) - exact(i)) <= 1e-3 * n
