"""cuOpt LP solvers (only where cuOpt is installed: a Python 3.13 venv on the GPU machine).

Every cuOpt solver must return a valid assignment (no car used twice); the vertex methods
(crossover, dual simplex) must also reach the exact optimum on these small cases. Rounded
PDLP is allowed a gap. At bench scale every cuOpt total is checked and its gap recorded.
"""

import numpy as np
import pytest
from test_solvers import synthetic_city

from fleetline_matcher.instances import build, objective
from fleetline_matcher.solvers import SOLVERS, scipy_sparse

CUOPT = [s for s in SOLVERS if s.startswith("cuopt")]
pytestmark = pytest.mark.skipif(not CUOPT, reason="cuOpt not installed (see matcher/README)")


@pytest.mark.parametrize("name", CUOPT)
@pytest.mark.parametrize("seed", range(5))
def test_valid_and_optimal(name, seed):
    inst = build(300, seed=seed, points=synthetic_city(seed))
    match = SOLVERS[name](inst)[0]
    used = match[match >= 0]
    assert len(used) == len(np.unique(used))
    got, ref = objective(inst, match), objective(inst, scipy_sparse(inst)[0])
    if name == "cuopt_pdlp":  # approximate by design
        assert got >= ref - 1e-6 * max(1.0, abs(ref))
    else:
        assert got == pytest.approx(ref, rel=1e-9)
