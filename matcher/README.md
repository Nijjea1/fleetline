# matcher/

Rider–car assignment solvers and **E4: how does solve time grow with batch size?**

```bash
cd core && go build -o bin/assignbench ./cmd/assignbench && cd ../matcher
uv run python -m fleetline_matcher.bench      # -> bench/e4/results/<machine>.json, bench/e4/e4.png
```

- **Instances** (`instances.py`): riders at real 8 AM pickup points, cars (2× riders) at real drop-off points, each rider linked to its 8 nearest cars within the 10-minute pickup cap; cost = pickup seconds from the calibrated travel model. Same shape as a real dispatch round, scaled up.
- **Solvers**: Fleetline's Go solver (`core/internal/assign`, successive shortest paths), SciPy `min_weight_full_bipartite_matching` (sparse LAPJV), SciPy `linear_sum_assignment` (dense, ≤ 3k riders: memory), OR-Tools min-cost flow. Every run checks that **all solvers reach the same optimal total**; only the solve call is timed.
- **GPU**: custom CUDA solver for the RTX 5060 (next).

## E4 results (M5 MacBook Pro, CPU; median of 3 seeds) → `bench/e4/results/cpu_arm64_darwin.json`

| Riders per batch | Go SSP (Fleetline) | SciPy sparse | OR-Tools flow | SciPy dense |
| --- | --- | --- | --- | --- |
| 1,000 | **0.40 ms** | 1.4 ms | 7.5 ms | 5.5 ms |
| 10,000 | **7.9 ms** | 337 ms | 148 ms | — (memory) |
| 30,000 | **25 ms** | 2.66 s | 630 ms | — |
| 100,000 | **168 ms** | 36.4 s | 4.76 s | — |

Why the gap: in a city, re-routing a rider only shuffles *nearby* cars, so each augmenting-path search stops after a few hops and total work grows ~n^1.1. General solvers don't exploit that locality. A real dispatch round is ~1,000 riders (≈0.4 ms), so CPU is already far from the bottleneck there; the GPU question is whether it can beat 25–170 ms at city-wide or multi-city batch sizes.

## GPU smoke test (Phase 1, checklist F)

On the RTX 5060 machine: `matcher/gpu_smoke/run.sh`. It prints the GPU, builds and checks a CUDA vector-add (correctness + memory bandwidth), and runs an fp16 matmul in PyTorch. Run on 2026-11-26 (Ubuntu 24.04 on WSL2, driver 591.84, CUDA 12.9); full output in `bench/gpu_smoke.txt`:

```
GPU: NVIDIA GeForce RTX 5060 Laptop GPU, compute 12.0, 26 SMs, 8.5 GB
vector add of 16777216 floats: 0.634 ms, 318 GB/s, correct
torch 2.11.0+cu128 cuda 12.8 | NVIDIA GeForce RTX 5060 Laptop GPU
fp16 8192^3 matmul: 89.5 ms = 12.3 TFLOPS
```

A smoke test, not a benchmark: one run of 10 matmuls including cuBLAS warm-up.
