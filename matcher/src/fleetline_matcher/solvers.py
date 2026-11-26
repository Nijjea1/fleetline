"""Solvers for E4. Every solver gets the same Instance and returns row -> col (-1 =
unmatched) minimizing sum(cost) + UNMATCHED per unmatched row, so their totals must agree.

Only the solve is timed; converting the edge list into each library's input format is
reported separately as setup, because a production system would keep data in that
format already.

cuda_auction (the GPU solver, matcher/cuda) is only present when it has been built there.
"""

import time

import numpy as np
from scipy.optimize import linear_sum_assignment
from scipy.sparse import coo_matrix, csr_matrix
from scipy.sparse.csgraph import min_weight_full_bipartite_matching

from .instances import UNMATCHED, Instance


def _dedupe(inst: Instance):
    """Keep the cheapest edge per (row, col)."""
    key = inst.rows.astype(np.int64) * inst.n_cols + inst.cols
    order = np.lexsort((inst.costs, key))
    first = np.ones(len(order), bool)
    first[1:] = key[order][1:] != key[order][:-1]
    o = order[first]
    return inst.rows[o], inst.cols[o], inst.costs[o]


def scipy_dense(inst: Instance):
    """Hungarian-family (LAPJV) on a dense n x (m + n) matrix: non-edges get a huge cost,
    and n extra 'unmatched' columns (one per row) cost UNMATCHED. Memory is O(n*m)."""
    t0 = time.perf_counter()
    rows, cols, costs = _dedupe(inst)
    n, m = inst.n_rows, inst.n_cols
    big = UNMATCHED * 10  # worse than staying unmatched, so a non-edge is never chosen
    mat = np.full((n, m + n), big)
    mat[rows, cols] = costs
    mat[np.arange(n), m + np.arange(n)] = UNMATCHED
    t1 = time.perf_counter()
    r, c = linear_sum_assignment(mat)
    t2 = time.perf_counter()
    match = np.full(n, -1)
    real = c < m
    match[r[real]] = c[real]
    return match, t1 - t0, t2 - t1


def scipy_sparse(inst: Instance):
    """LAPJVsp on the sparse edge list plus a diagonal of 'unmatched' columns. Costs get +1
    because the sparse format can't store explicit zeros as edges; every row is matched
    exactly once (possibly to its unmatched column), so the shift doesn't change the optimum."""
    t0 = time.perf_counter()
    rows, cols, costs = _dedupe(inst)
    n, m = inst.n_rows, inst.n_cols
    data = np.concatenate([costs + 1, np.full(n, UNMATCHED + 1)])
    ri = np.concatenate([rows, np.arange(n)])
    ci = np.concatenate([cols, m + np.arange(n)])
    g = csr_matrix(coo_matrix((data, (ri, ci)), shape=(n, m + n)))
    t1 = time.perf_counter()
    r, c = min_weight_full_bipartite_matching(g)
    t2 = time.perf_counter()
    match = np.full(n, -1)
    real = c < m
    match[r[real]] = c[real]
    return match, t1 - t0, t2 - t1


def ortools_flow(inst: Instance):
    """Google OR-Tools min-cost flow: source -> rider -> car -> sink, plus rider -> sink at
    UNMATCHED. Integer costs, so seconds are scaled to milliseconds."""
    from ortools.graph.python import min_cost_flow

    t0 = time.perf_counter()
    rows, cols, costs = _dedupe(inst)
    n, m = inst.n_rows, inst.n_cols
    src, sink = n + m, n + m + 1
    tails = np.concatenate([np.full(n, src), rows, np.arange(n), n + np.arange(m)])
    heads = np.concatenate([np.arange(n), n + cols, np.full(n, sink), np.full(m, sink)])
    unit = np.concatenate([np.zeros(n), np.round(costs * 1000), np.full(n, UNMATCHED * 1000), np.zeros(m)]).astype(
        np.int64
    )
    f = min_cost_flow.SimpleMinCostFlow()
    arcs = f.add_arcs_with_capacity_and_unit_cost(tails, heads, np.ones(len(tails), np.int64), unit)
    f.set_nodes_supplies(np.array([src, sink]), np.array([n, -n]))
    t1 = time.perf_counter()
    status = f.solve()
    t2 = time.perf_counter()
    if status != f.OPTIMAL:
        raise RuntimeError(f"OR-Tools status {status}")
    flows = f.flows(arcs[n : n + len(rows)])
    match = np.full(n, -1)
    used = flows > 0
    match[rows[used]] = cols[used]
    return match, t1 - t0, t2 - t1


try:
    from . import _cuda_auction
except ImportError:  # not built (no CUDA here, e.g. CI or the Mac)
    _cuda_auction = None

CUDA_STATS: dict = {}  # phases, rounds and the timing split of the last cuda_auction call
_cuda_warm = False


def cuda_auction(inst: Instance):
    """Forward auction with epsilon-scaling on the GPU (matcher/cuda/auction.cu). Costs are
    rounded to whole milliseconds like OR-Tools. Setup includes building the GPU's input and
    both host<->GPU copies; solve is the auction itself."""
    global _cuda_warm
    if not _cuda_warm:  # first call pays for CUDA context and kernel loading: keep it out of timings
        _cuda_auction.solve(1, 1, np.zeros(1, np.int32), np.zeros(1, np.int32), np.ones(1, np.int64), 10**9)
        _cuda_warm = True
    t0 = time.perf_counter()
    rows, cols, costs = _dedupe(inst)
    ms = np.round(costs * 1000).astype(np.int64)
    t1 = time.perf_counter()
    match, st = _cuda_auction.solve(inst.n_rows, inst.n_cols, rows, cols, ms, round(UNMATCHED * 1000))
    CUDA_STATS.clear()
    CUDA_STATS.update(st)
    return match.astype(np.int64), (t1 - t0) + st["setup_s"] + st["copy_back_s"], st["solve_s"]


SOLVERS = {"scipy_dense": scipy_dense, "scipy_sparse": scipy_sparse, "ortools_flow": ortools_flow}
if _cuda_auction is not None:
    SOLVERS["cuda_auction"] = cuda_auction
# Solvers that work on millisecond-rounded costs: their totals may differ by up to 1 ms per row.
ROUNDED = {"ortools_flow", "cuda_auction"}
