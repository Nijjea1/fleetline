# GPU machine (RTX 5060): setup and runs

Roadmap items 2.7–2.9 and 3.3 need an NVIDIA GPU. Run them in this order; each step's output goes into the repo.

## 1. One-time setup (about 30–60 min)
The RTX 50 series (Blackwell) needs **CUDA 12.8 or newer** and a recent driver.

**If the machine runs Windows, use WSL2 (Ubuntu).** cuOpt is Linux-only, and TensorRT and the build tools are simpler there.
1. Install or update the **NVIDIA Windows driver** (Game Ready or Studio, current release). That's the only driver needed: WSL uses it, so don't install a Linux driver inside WSL.
2. PowerShell (admin): `wsl --install -d Ubuntu-24.04`, then reboot and open Ubuntu.
3. In Ubuntu, install the **CUDA toolkit for WSL-Ubuntu** (12.8+) from NVIDIA's CUDA downloads page (choose Linux → x86_64 → WSL-Ubuntu → deb (network)). Then:
   `echo 'export PATH=/usr/local/cuda/bin:$PATH' >> ~/.bashrc && source ~/.bashrc`
4. Check: `nvidia-smi` shows the RTX 5060, and `nvcc --version` says 12.8+.

**Then, in Ubuntu (or native Linux):**
```bash
sudo apt update && sudo apt install -y git build-essential gh
curl -LsSf https://astral.sh/uv/install.sh | sh && source ~/.bashrc
gh auth login                       # the repo is private
gh repo clone Nijjea1/fleetline && cd fleetline
```

## 2. Copy the data the scripts need (not in git)
From the Mac, copy these into the same paths on the GPU machine (USB, AirDrop to a phone, Google Drive, or `scp`):

| File | Size | Used by |
| --- | --- | --- |
| `data/models/eta_residual_mlp.onnx` | 9 MB | TensorRT (step 4) |
| `data/derived/day_2026-09-16_seed7.parquet` | 29 MB | E4 GPU instances (step 5) |
| `data/derived/travel_model_2026-09-16.json` | 4 KB | E4 GPU instances (step 5) |

## 3. Smoke test (2.7): 5 minutes
```bash
bash matcher/gpu_smoke/run.sh 2>&1 | tee gpu_smoke.txt
```
Passes if it prints the GPU name, a `vector add … correct` line and a PyTorch fp16 matmul TFLOPS line. Send `gpu_smoke.txt` back (paste it into Claude Code).

## 4. TensorRT, E6 (3.3): 15 minutes
Install TensorRT 10.x for your CUDA version (NVIDIA's apt repo: `sudo apt install tensorrt`, which includes `trtexec`; it may live in `/usr/src/tensorrt/bin`, so add that to PATH). Then:
```bash
bash ml/tensorrt/bench.sh 2>&1 | tee e6_tensorrt.txt
```
Send `e6_tensorrt.txt` back, with `nvidia-smi` and `trtexec --version`. Plug in the laptop and keep it otherwise idle: these are timing benchmarks.

## 5. CUDA assignment solver, E4 GPU column (2.8)
Not written yet: GPU code needs to be compiled and debugged on the GPU. **Best way:** install Claude Code on the GPU machine and open this repo there. `docs/ROADMAP.md`, `docs/PROGRESS.md` and `matcher/README.md` carry the context. Plan: an auction-algorithm solver in CUDA (parallel bids per rider, epsilon scaling), pybind11 into `fleetline_matcher`, checked against the same optimal totals as the CPU solvers, profiled with Nsight Compute. **Target to beat: Go SSP at 25 ms (30k riders) and 168 ms (100k).**

## 6. cuOpt (2.9): after 5
`pip install cuopt-cu12` on Linux/WSL. It joins E4 as one more solver column.
