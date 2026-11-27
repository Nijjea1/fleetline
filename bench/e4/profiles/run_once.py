"""One cuda_auction solve on a real E4 instance, for Nsight (bench/e4/profiles/profile.sh).

python run_once.py N [SEED]     warm up CUDA, then solve N riders once
python run_once.py --warm       print how many kernel launches the warm-up makes
"""

import sys

import numpy as np

from fleetline_matcher import _cuda_auction
from fleetline_matcher.instances import UNMATCHED, build
from fleetline_matcher.solvers import CUDA_STATS, cuda_auction


def warm_up() -> dict:
    one = np.zeros(1, np.int32)
    return _cuda_auction.solve(1, 1, one, one, np.ones(1, np.int64), round(UNMATCHED * 1000))[1]


if sys.argv[1] == "--warm":
    print(warm_up()["phases"])
    sys.exit()
n, seed = int(sys.argv[1]), int(sys.argv[2]) if len(sys.argv) > 2 else 0
inst = build(n, seed=seed)
warm_up()
_, setup, solve = cuda_auction(inst)
stats = {k: v for k, v in CUDA_STATS.items() if k != "phase_rounds"}
print(f"n={n} seed={seed} setup {setup * 1e3:.1f} ms solve {solve * 1e3:.1f} ms {stats}")
