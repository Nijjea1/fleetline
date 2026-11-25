# matcher/

Rider–driver assignment solvers. Phase 2.

- CPU baselines: SciPy Hungarian (`linear_sum_assignment`), Google OR-Tools.
- GPU: custom CUDA kernel for cost matrix + assignment, exposed to Python with pybind11. Built on the RTX 5060 machine; CPU paths run anywhere.

## GPU smoke test (Phase 1, checklist F)

On the RTX 5060 machine: `matcher/gpu_smoke/run.sh`. It prints the GPU, builds and checks a CUDA vector-add (correctness + memory bandwidth), and runs an fp16 matmul in PyTorch. **Not yet run**; paste the output here when it is.
