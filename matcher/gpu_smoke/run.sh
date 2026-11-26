#!/usr/bin/env bash
# Checklist F1 + F2 on the RTX 5060 machine. Paste the output into matcher/README.md.
set -euo pipefail
cd "$(dirname "$0")"
nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv
nvcc --version | tail -1
nvcc -O2 -arch=native -o vecadd vecadd.cu && ./vecadd
# F2: PyTorch with CUDA. ml/'s lockfile gives Linux the CPU-only wheel (for CI), so swap
# in the CUDA 12.8 build (needed for the RTX 50 series) and run without re-syncing.
cd ../../ml && uv sync -q
uv pip install -q --reinstall torch --index-url https://download.pytorch.org/whl/cu128
uv run --no-sync python - <<'PY'
import time, torch
assert torch.cuda.is_available(), "PyTorch can't see a CUDA GPU"
print("torch", torch.__version__, "cuda", torch.version.cuda, "|", torch.cuda.get_device_name(0))
a = torch.randn(8192, 8192, device="cuda", dtype=torch.float16)
torch.cuda.synchronize(); t = time.perf_counter()
for _ in range(10): b = a @ a
torch.cuda.synchronize(); dt = (time.perf_counter() - t) / 10
print(f"fp16 8192^3 matmul: {dt*1e3:.1f} ms = {2*8192**3/dt/1e12:.1f} TFLOPS")
PY
