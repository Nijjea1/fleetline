"""Solvers for E4. Every solver gets the same Instance and returns row -> col (-1 =
unmatched) minimizing sum(cost) + UNMATCHED per unmatched row, so their totals must agree.

Only the solve is timed; converting the edge list into each library's input format is
reported separately as setup, because a production system would keep data in that
format already.

cuda_auction (the GPU solver, matcher/cuda) is only present when it has been built there.
The cuopt_* solvers (NVIDIA cuOpt's LP solvers) only appear where cuOpt is installed: it
needs Python <= 3.13 and an NVIDIA GPU, so it runs from a separate venv (matcher/README).
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


try:
    from cuopt.linear_programming import DataModel, Solve, SolverMethod, SolverSettings
except ImportError:  # not installed (Python 3.14 project venv, the Mac, CI)
    Solve = None

CUOPT_STATS: dict = {}  # termination status, cuOpt's own solve time, fractional edges
_TIGHT = 1e-10  # PDLP's default tolerances (~1e-4 relative) stop far from a vertex; see D-019


def _cuopt_lp(inst: Instance, method: str, extra: dict):
    """The assignment as an LP: a variable x_e in [0, 1] per (rider, car) edge plus u_i per
    rider (unmatched, cost UNMATCHED). Rider rows: sum x + u = 1. Car rows: sum x <= 1. The
    constraint matrix is totally unimodular, so every *vertex* optimum is 0/1 and equals the
    assignment optimum. Rows are matched where x_e > 0.5; the bench checks the total
    against Go, because an interior (non-vertex) LP solution can round to a worse one."""
    t0 = time.perf_counter()
    rows, cols, costs = _dedupe(inst)
    n, m, e = inst.n_rows, inst.n_cols, len(rows)
    r = np.concatenate([rows, np.arange(n), n + cols])
    c = np.concatenate([np.arange(e), e + np.arange(n), np.arange(e)])
    a = csr_matrix((np.ones(len(r)), (r, c)), shape=(n + m, e + n))
    dm = DataModel()
    dm.set_csr_constraint_matrix(a.data, a.indices.astype(np.int32), a.indptr.astype(np.int32))
    dm.set_constraint_lower_bounds(np.concatenate([np.ones(n), np.full(m, -np.inf)]))
    dm.set_constraint_upper_bounds(np.ones(n + m))
    dm.set_objective_coefficients(np.concatenate([costs, np.full(n, UNMATCHED)]))
    dm.set_variable_lower_bounds(np.zeros(e + n))
    dm.set_variable_upper_bounds(np.ones(e + n))
    st = SolverSettings()
    st.set_parameter("method", getattr(SolverMethod, method))
    st.set_parameter("log_to_console", False)
    st.set_parameter("time_limit", 600)
    for k, v in extra.items():
        st.set_parameter(k, v)
    t1 = time.perf_counter()
    sol = Solve(dm, st)
    t2 = time.perf_counter()
    x = np.asarray(sol.get_primal_solution())[:e]
    # Round: take edges with x > 0.5, largest first, at most one per rider and per car. A
    # vertex solution is already 0/1; an interior one is only feasible to a tolerance, so two
    # edges into one car can both sit just above 0.5 (seen at 30k riders).
    match = np.full(n, -1)
    taken = np.zeros(m, bool)
    for i in np.flatnonzero(x > 0.5)[np.argsort(-x[x > 0.5], kind="stable")]:
        if match[rows[i]] < 0 and not taken[cols[i]]:
            match[rows[i]], taken[cols[i]] = cols[i], True
    CUOPT_STATS.clear()
    CUOPT_STATS.update(
        status=int(sol.get_termination_status()),
        cuopt_solve_s=float(sol.get_solve_time()),
        fractional=int(((x > 1e-6) & (x < 1 - 1e-6)).sum()),
    )
    return match, t1 - t0, t2 - t1


def cuopt_pdlp(inst: Instance):
    """cuOpt PDLP (first-order LP method, on the GPU) at tight tolerances, rounded."""
    tol = {f"{a}_{b}_tolerance": _TIGHT for a in ("relative", "absolute") for b in ("gap", "primal", "dual")}
    return _cuopt_lp(inst, "PDLP", tol)


def cuopt_pdlp_crossover(inst: Instance):
    """cuOpt PDLP followed by crossover to a vertex: exact by construction."""
    return _cuopt_lp(inst, "PDLP", {"crossover": True})


def cuopt_dual_simplex(inst: Instance):
    """cuOpt's dual simplex (runs on the CPU): exact vertex solution."""
    return _cuopt_lp(inst, "DualSimplex", {})


SOLVERS = {"scipy_dense": scipy_dense, "scipy_sparse": scipy_sparse, "ortools_flow": ortools_flow}
if _cuda_auction is not None:
    SOLVERS["cuda_auction"] = cuda_auction
if Solve is not None:
    SOLVERS |= {
        "cuopt_pdlp": cuopt_pdlp,
        "cuopt_pdlp_crossover": cuopt_pdlp_crossover,
        "cuopt_dual_simplex": cuopt_dual_simplex,
    }
# Checked against Go but not trusted to be exact: the bench records the gap instead of
# stopping. PDLP is approximate by design; crossover and dual simplex should return a vertex
# (exact here), but PDLP + crossover returned a non-optimal total at 100k riders (D-019).
APPROX = {"cuopt_pdlp", "cuopt_pdlp_crossover", "cuopt_dual_simplex"}
# Solvers that work on millisecond-rounded costs: their totals may differ by up to 1 ms per row.
ROUNDED = {"ortools_flow", "cuda_auction"}
