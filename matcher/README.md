# matcher/

Rider–car assignment solvers and **E4: how does solve time grow with batch size?**

```bash
cd core && go build -o bin/assignbench ./cmd/assignbench && cd ../matcher
uv run python -m fleetline_matcher.bench      # -> bench/e4/results/<machine>.json, bench/e4/e4.png
```

- **Instances** (`instances.py`): riders at real 8 AM pickup points, cars (2× riders) at real drop-off points, each rider linked to its 8 nearest cars within the 10-minute pickup cap; cost = pickup seconds from the calibrated travel model. Same shape as a real dispatch round, scaled up.
- **Solvers**: Fleetline's Go solver (`core/internal/assign`, successive shortest paths), SciPy `min_weight_full_bipartite_matching` (sparse LAPJV), SciPy `linear_sum_assignment` (dense, ≤ 3k riders: memory), OR-Tools min-cost flow. Every run checks that **all solvers reach the same optimal total**; only the solve call is timed.
- **GPU**: `cuda_auction`, a CUDA forward-auction solver (`cuda/`), only present where it has been built (needs `nvcc`; CI and the Mac skip it).

## E4 results (M5 MacBook Pro, CPU; median of 3 seeds) → `bench/e4/results/cpu_arm64_darwin.json`

| Riders per batch | Go SSP (Fleetline) | SciPy sparse | OR-Tools flow | SciPy dense |
| --- | --- | --- | --- | --- |
| 1,000 | **0.40 ms** | 1.4 ms | 7.5 ms | 5.5 ms |
| 10,000 | **7.9 ms** | 337 ms | 148 ms | — (memory) |
| 30,000 | **25 ms** | 2.66 s | 630 ms | — |
| 100,000 | **168 ms** | 36.4 s | 4.76 s | — |

Why the gap: in a city, re-routing a rider only shuffles *nearby* cars, so each augmenting-path search stops after a few hops and total work grows ~n^1.1. General solvers don't exploit that locality. A real dispatch round is ~1,000 riders (≈0.4 ms), so CPU is already far from the bottleneck there; the GPU question is whether it can beat 25–170 ms at city-wide or multi-city batch sizes.

## E4 on the RTX 5060 machine, including the GPU (median of 3 seeds) → `bench/e4/results/rtx5060_wsl2.json`

```bash
bash matcher/cuda/build.sh                      # CMake + pybind11 -> src/fleetline_matcher/_cuda_auction*.so
cd matcher && uv run pytest -q tests/test_cuda.py
uv run python -m fleetline_matcher.bench --machine rtx5060_wsl2
```

RTX 5060 Laptop GPU (8 GB, sm_120) + Ryzen AI 9 HX 370, Ubuntu 24.04 on WSL2, driver 591.84, CUDA 12.9. Plugged in, Windows "Performance" plan, otherwise idle. All CPU solvers were re-run here, so every column is the same machine.

| Riders per batch | Go SSP | SciPy sparse | OR-Tools flow | **GPU auction** (spread over seeds) |
| --- | --- | --- | --- | --- |
| 1,000 | **1.6 ms** | 4.0 ms | 28.6 ms | 250 ms (236–298) |
| 3,000 | **8.3 ms** | 34.1 ms | 121 ms | 1.74 s (0.85–1.86) |
| 10,000 | **32.5 ms** | 446 ms | 662 ms | 4.95 s (2.12–9.34) |
| 30,000 | **121 ms** | 3.89 s | 3.00 s | 22.7 s (12.6–24.1) |
| 100,000 | **649 ms** | 50.5 s | 18.0 s | 1.18 s (1.07–1.21) |

**No crossover with Go: the GPU loses at every size.** At 100k riders it is 1.8× slower than Go, but **15× faster than OR-Tools and 43× faster than SciPy** (end to end including host↔GPU copies: 1.39–1.56 s). This machine's CPU runs Go ~4× slower than the M5 (649 vs 168 ms at 100k), which is why the comparison above is same-machine.

**How it works** (`cuda/auction.cu`, [D-018](../docs/DECISIONS.md), [learning note 10](../docs/learning/10-gpu-auction.md)): Bertsekas' forward auction with ε-scaling (θ = 8). All unassigned bidders bid at once; `atomicMax` picks each car's highest bid and `atomicMin` on the id breaks ties. Cars outnumber riders, so it solves an equivalent square problem (a "slack" bidder per car) to keep auction's optimality guarantee. Costs are whole ms × (N + 1), so the last phase is exact; a per-phase duality-gap check stops early when it already proves the optimum. Each ε phase is one cooperative kernel (grid barriers between bid / resolve / award), and block 0 finishes alone once ≤ 1,024 bidders remain. Host↔GPU copies are reported as setup, like the CPU solvers' format conversion.

**Why it loses: the profile** (`bench/e4/profiles/`, seed 0):

| Riders | Rounds, whole grid / tail (block 0 alone) | Tail share of GPU cycles | Bids per round |
| --- | --- | --- | --- |
| 1,000 | 24 / 129,350 | 99.9% | ~4 |
| 30,000 | 1,654 / 6.0 M | 99.8% | ~5 |
| 100,000 | 3,409 / 0.33 M | 93% | ~105 |

Nsight Compute on a mid-run phase: at 30k, SMs are active **4.3%** of cycles (about one of 26), compute 0.17% and memory 0.36% of peak, achieved occupancy 25%; at 100k, 12.4% SM active, 3.7% memory, 65% occupancy. Nsight Systems: solve time is the CPU waiting on the phase kernels; launches and copies are a few ms. The work is **eviction chains** (A takes B's car, B takes C's…), one step per round with a handful of bidders, so the GPU is latency-bound with almost every SM idle. At 100k only 92% of riders can be matched, competition is everywhere, and each round settles ~100 bids: 18× fewer rounds for about the same number of bids, hence the dip. SSP does the same chain as one Dijkstra on one core, and city geography keeps it short.

What might change the answer: a hybrid (GPU auction for the parallel early phases, CPU SSP to finish from its prices), forward/reverse auction for rectangular problems (no slack bidders), or a barrier-free warp for the tail.

## GPU smoke test (Phase 1, checklist F)

On the RTX 5060 machine: `matcher/gpu_smoke/run.sh`. It prints the GPU, builds and checks a CUDA vector-add (correctness + memory bandwidth), and runs an fp16 matmul in PyTorch. Run on 2026-11-26 (Ubuntu 24.04 on WSL2, driver 591.84, CUDA 12.9); full output in `bench/gpu_smoke.txt`:

```
GPU: NVIDIA GeForce RTX 5060 Laptop GPU, compute 12.0, 26 SMs, 8.5 GB
vector add of 16777216 floats: 0.634 ms, 318 GB/s, correct
torch 2.11.0+cu128 cuda 12.8 | NVIDIA GeForce RTX 5060 Laptop GPU
fp16 8192^3 matmul: 89.5 ms = 12.3 TFLOPS
```

A smoke test, not a benchmark: one run of 10 matmuls including cuBLAS warm-up.
