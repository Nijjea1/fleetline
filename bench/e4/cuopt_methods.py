"""E4 side experiment: which cuOpt LP settings actually return the assignment optimum?

    cd matcher && ~/cuopt_probe/.venv/bin/python ../bench/e4/cuopt_methods.py > ../bench/e4/cuopt_methods.txt

cuOpt reports "optimal" whenever its tolerances are met; the assignment is only solved if
the LP solution is a vertex (all 0/1). For each method we print cuOpt's status, how many
edge variables are fractional, and the rounded total minus the exact optimum (SciPy).
Seed 0, one run each: this is about correctness, the
timing comparison is bench/e4/results/rtx5060_wsl2_cuopt.json.
"""

import platform
import time

from fleetline_matcher.instances import build, objective
from fleetline_matcher.solvers import _TIGHT, CUOPT_STATS, _cuopt_lp, scipy_sparse

TIGHT = {f"{a}_{b}_tolerance": _TIGHT for a in ("relative", "absolute") for b in ("gap", "primal", "dual")}
CONFIGS = [
    ("PDLP", "default tolerances", {}),
    ("PDLP", "tight tolerances", TIGHT),
    ("PDLP", "crossover", {"crossover": True}),
    ("Barrier", "default", {}),
    ("Barrier", "crossover", {"crossover": True}),
    ("DualSimplex", "default", {}),
]

print(f"# {time.strftime('%Y-%m-%d')} {platform.platform()}")
print("n       method       setting              wall_s  status  fractional  rounded_gap  status=1 means optimal")
for n in (1000, 10000):
    inst = build(n, seed=0)
    ref = objective(inst, scipy_sparse(inst)[0])
    for method, label, extra in CONFIGS:
        match, _, wall = _cuopt_lp(inst, method, extra)
        gap = objective(inst, match) - ref
        print(
            f"{n:<7} {method:<12} {label:<20} {wall:7.3f}  {CUOPT_STATS['status']:>6}  "
            f"{CUOPT_STATS['fractional']:>10}  {gap:+11.3f}",
            flush=True,
        )
