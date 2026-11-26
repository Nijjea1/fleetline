# GPU machine runbook (RTX 5060)

**For Claude Code running on the GPU machine.** Work through the tasks below in order. Each task has a goal, exact commands, a pass check and what to record. Stop and ask the user only where a step says **ASK** (things that need their password, account, or a file from their Mac). Everything else: do it, verify it, commit it.

Read `CLAUDE.md` (project rules) first. Context, if needed: `docs/ROADMAP.md` (what's left), `docs/PROGRESS.md` (history), `matcher/README.md` (E4 so far), `ml/README.md` (E5/E6 so far).

## Ground rules for this machine
- **No number goes into the README or `docs/RESUME_CONTEXT.md` unless a command here produced it** and its raw output is committed under `bench/` or `ml/results/`. Losing results get recorded too ("the GPU only wins above N" is a good result).
- **Timing benchmarks:** the machine must be plugged in, on a high-performance power plan, with nothing else heavy running. Check with the user once before the first timed run (**ASK**). Correctness runs don't need this.
- **Record the environment** with every result: GPU name, driver, CUDA version (`nvidia-smi`, `nvcc --version`), and TensorRT/PyTorch versions where relevant.
- Small commits, each with a clear message ending in the co-author line from `CLAUDE.md`. Push after each task. Never rewrite history or set commit dates.
- Keep the user informed in plain language: what you're running, how long it takes, what the result means.

---

## Task 0. Environment check and setup
**Goal:** the CUDA toolchain, `uv`, Go and the repo all work.

1. Detect the platform: `uname -a`. If you are in **Windows (PowerShell/cmd), not WSL**: tell the user that cuOpt and the build tools need Linux, and ask them to install WSL2 (**ASK**: needs admin): `wsl --install -d Ubuntu-24.04`, reboot, then re-clone the repo inside Ubuntu and restart Claude Code there. Don't install a Linux NVIDIA driver inside WSL; the Windows driver serves WSL.
2. Check the GPU: `nvidia-smi`. Must list an RTX 5060. If the command is missing or fails, the NVIDIA driver is missing or old: ask the user to install the current driver from nvidia.com (**ASK**).
3. Check the toolkit: `nvcc --version` must be **12.8 or newer** (Blackwell/RTX 50 needs it). If it's missing or older, install the CUDA toolkit for this distro from NVIDIA's apt repo (on WSL choose the *WSL-Ubuntu* package, which ships no driver). This needs `sudo` (**ASK** the user to run it, or to give you a terminal where sudo works). Then make sure `/usr/local/cuda/bin` is on PATH.
4. Tools: `git`, `build-essential`, `uv` (`curl -LsSf https://astral.sh/uv/install.sh | sh`) and Go at the version in `core/go.mod`. Install whatever is missing.
5. Build and test the CPU side: `cd core && go build -o bin/assignbench ./cmd/assignbench && go test ./internal/assign/ && cd ../matcher && uv sync && uv run pytest -q`.

**Pass:** all of the above succeed. **Record:** nothing yet.

## Task 1. Data files (not in git)
The benchmarks need these files at these paths:

| File | Used by |
| --- | --- |
| `data/models/eta_residual_mlp.onnx` | Task 3 (TensorRT) |
| `data/derived/day_2026-09-16_seed7.parquet` | Tasks 4–5 (E4 instances) |
| `data/derived/travel_model_2026-09-16.json` | Tasks 4–5 (E4 instances) |

1. If they're present, continue.
2. If the two `data/derived/` files are missing, rebuild them. The pipeline is deterministic (about 510 MB download, a few minutes):
   `cd sim && uv run fleetline-sim download && uv run fleetline-sim prepare && uv run fleetline-sim calibrate && cd ..`
   Then confirm both files exist.
3. The ONNX model can't be cheaply rebuilt (it needs the full ML training pipeline). If it's missing, **ASK** the user to copy `data/models/eta_residual_mlp.onnx` (9 MB) from the Mac. Meanwhile, do Tasks 2, 4 and 5 and come back to Task 3.

## Task 2. Smoke test (roadmap 2.7, checklist F1/F2)
```bash
bash matcher/gpu_smoke/run.sh 2>&1 | tee bench/gpu_smoke.txt
```
**Pass:** the output shows:
- the GPU name and compute capability (12.x for RTX 50),
- `vector add of … floats: … correct`,
- a `torch … cuda …` line naming the RTX 5060,
- an fp16 matmul TFLOPS line.

The script swaps in the CUDA 12.8 PyTorch wheel because `ml/`'s lockfile installs CPU torch on Linux (for CI).

**Record:**
- Commit `bench/gpu_smoke.txt`.
- Paste the key lines into `matcher/README.md` under a "GPU smoke test" heading.
- Mark F1/F2 done in `docs/PHASE1_CHECKLIST.md` and 2.7 done in `docs/ROADMAP.md`.

**If it fails:**
- `nvcc fatal: Unsupported gpu architecture` → the toolkit is too old (back to Task 0).
- `torch.cuda.is_available()` is False → the wrong wheel or driver. Check with `uv run --no-sync python -c "import torch;print(torch.__version__, torch.version.cuda)"`.

## Task 3. TensorRT inference, E6 (roadmap 3.3)
**Goal:** latency of the ETA model under TensorRT FP32 and FP16 at batch 1–8,192, to set beside the Mac numbers in `ml/results/e6_inference_mac.json`.

1. Install TensorRT 10.x matching the CUDA version (NVIDIA apt repo: `sudo apt install tensorrt`, **ASK** for sudo). `trtexec` usually lands in `/usr/src/tensorrt/bin`, so add that to PATH. Check: `trtexec --help | head -1`.
2. **ASK** the user to confirm the machine is plugged in and otherwise idle.
3. Run `bash ml/tensorrt/bench.sh 2>&1 | tee ml/results/e6_tensorrt_raw.txt`.
4. Parse the raw output into `ml/results/e6_tensorrt.json`. Use the same shape as `e6_inference_mac.json` where possible: per batch × precision, include the GPU compute time median and p99 and the throughput, plus an `env` block with GPU, driver, CUDA and TensorRT versions.
5. **Accuracy check:** also confirm FP16 gives the same test MAE as FP32/ONNX within 0.5 s. That needs a small script that runs the engine on the test split: use `ml/`'s dataset code and TensorRT's Python API (`uv pip install tensorrt` matching the system version). If that turns into a large job, record the latency results first and note accuracy as pending.
6. **INT8 (optional, later):** needs a calibrator on about 10k validation rows. Only do it after FP32/FP16 are committed.

**Record:**
- A table in `ml/README.md` (E6 section): batch × {ONNX Runtime CPU on the Mac, M5 GPU, TensorRT FP32, TensorRT FP16}.
- Mark 3.3 in `docs/ROADMAP.md`.
- Add a resume bullet to `docs/RESUME_CONTEXT.md` only from these numbers, and move "TensorRT" from "Planned" to "Built".

## Task 4. CUDA assignment solver, E4 GPU column (roadmap 2.8). The main task
**The question E4 asks:** when does a GPU beat the CPU at assigning riders to cars?

**Current CPU results** (M5 MacBook, `bench/e4/results/cpu_arm64_darwin.json`):

| Riders per batch | Go SSP | OR-Tools | SciPy sparse |
| --- | --- | --- | --- |
| 1,000 | 0.40 ms | 7.5 ms | 1.4 ms |
| 30,000 | 25 ms | 630 ms | 2.66 s |
| 100,000 | 168 ms | 4.76 s | 36.4 s |

**The target is Go SSP, not SciPy.**

**The problem:**
- An `Instance` (`matcher/src/fleetline_matcher/instances.py`) is a sparse bipartite graph: `n_rows` riders and `n_cols` cars (2× riders).
- Each rider has edges to up to 8 nearest cars, with costs in seconds (float64).
- Minimize Σ edge costs + `UNMATCHED` (1e6) per unmatched rider. Leaving a rider unmatched is always allowed, so every instance is feasible.
- The binary file format is in the `instances.py` docstring; `core/cmd/assignbench` reads it.

**Build:**
1. **Algorithm: forward auction with ε-scaling** (Bertsekas). It is the standard GPU-friendly exact method for sparse assignment.
   - Riders bid in parallel for their best car (best and second-best value over ≤ 8 edges plus the "stay unmatched" option at `UNMATCHED`). Cars accept the highest bid with `atomicMax` on a packed (bid, rider) 64-bit word.
   - Scale ε down by a factor (e.g. 4–8) each phase, to below 1/n of the cost resolution.
   - **Exactness:** auction is optimal when ε < δ/n, where δ is the cost granularity. Costs are float seconds, so work on integers: multiply costs by 1000, round to int64 (ms), and use final ε < 1/n in those units. That's the same rounding OR-Tools uses, so verify against the optimum with the same tolerance the bench already applies to OR-Tools (1e-3 × n).
   - Model "unmatched" as a private dummy car per rider at cost `UNMATCHED`, never contested.
   - Do the sparse graph in CSR (row offsets, col idx, int64 costs) on the device.
   - Keep the host↔device copy outside `solve_s` but report it as `setup_s`, exactly like the CPU solvers do. Report the end-to-end time too, in the notes.
2. **Code layout:**
   - `matcher/cuda/auction.cu` holds the kernels and a host `solve(...)`.
   - `matcher/cuda/auction_py.cpp` holds pybind11 bindings taking NumPy arrays and returning `(match int32[n_rows], setup_s, solve_s)`.
   - Build with CMake or `scikit-build-core` as an optional extra. **CI has no GPU**, so the CUDA build and tests must be skipped cleanly when CUDA isn't available.
   - Register it in `SOLVERS` in `solvers.py` as `cuda_auction` (import guarded, so it's only present when built).
3. **Correctness first** (before any timing):
   - Add tests in `matcher/tests/test_cuda.py`, skipped without a GPU. They must check that `cuda_auction` matches `scipy_sparse` totals within the OR-Tools tolerance on synthetic cities (reuse `synthetic_city` from `tests/test_solvers.py`) and on random small dense cases.
   - Include the empty instance, a single rider, and the "greedy trap" and long-augmenting-path cases from `core/internal/assign/assign_test.go`.
4. **Benchmark:**
   - Run `cd matcher && uv run python -m fleetline_matcher.bench --machine rtx5060_<os>`. The bench already verifies every solver's total against Go before recording times.
   - Run the Go and CPU solvers here too, so the GPU column has a same-machine CPU comparison (this machine's CPU differs from the M5).
   - The result file goes to `bench/e4/results/rtx5060_<os>.json`. The plot includes `cuda` in red, so rename the solver key or adjust the colors map to match.
   - Make sure `bench/e4/e4.png` shows both machines clearly, either with separate line styles per machine or as a second figure `e4_gpu.png`.
5. **Profile:**
   - Run Nsight Systems (`nsys profile`) on the 30k and 100k cases to see the kernel vs copy vs launch-overhead split.
   - Run Nsight Compute (`ncu`) on the bidding kernel for occupancy and memory throughput.
   - Save the summaries as text in `bench/e4/profiles/`. They are the answer to "why does the GPU win or lose at size N".
6. **Write it up:**
   - Add a section to `matcher/README.md` and learning note `docs/learning/10-gpu-auction.md`: what auction is, why it's parallel, the crossover size, and what the profile showed.
   - Update the README E4 section and `RESUME_CONTEXT.md` (move CUDA from "Planned" to "Built" with the measured numbers, win or lose).

**Pass:** totals match at every size, and timings are recorded for every size the GPU memory allows (8 GB should fit 100k easily).

## Task 5. NVIDIA cuOpt (roadmap 2.9)
After Task 4:
1. Install cuOpt for this CUDA version (`uv pip install` of NVIDIA's `cuopt` CUDA 12 package; check NVIDIA's current install docs for the exact name).
2. Model the instance as a linear assignment or min-cost flow, whichever cuOpt supports.
3. Add a `cuopt` solver to `SOLVERS` and re-run the bench.
4. If cuOpt has no fitting API for this problem (it targets routing and LP/MILP), record that honestly in `matcher/README.md` and use its LP solver on the assignment LP as the comparison.

## Task 6. Wrap up
- Update `docs/ROADMAP.md` (2.7–2.9, 3.3) and add a dated entry to `docs/PROGRESS.md` covering what ran, the results, surprises and versions.
- Update `docs/RESUME_CONTEXT.md` (Built vs Planned) and the README result sections, using only numbers from committed result files.
- Push. Then tell the user, in plain language, what got faster, where the GPU wins and loses, and the new resume bullets.
