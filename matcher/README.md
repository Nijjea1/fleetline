# matcher/

Rider–driver assignment solvers. Phase 2.

- CPU baselines: SciPy Hungarian (`linear_sum_assignment`), Google OR-Tools.
- GPU: custom CUDA kernel for cost matrix + assignment, exposed to Python with pybind11. Built on the RTX 5060 machine; CPU paths run anywhere.
