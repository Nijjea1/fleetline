#!/usr/bin/env bash
# Build the CUDA auction solver into src/fleetline_matcher/_cuda_auction*.so.
# Needs nvcc (CUDA 12.8+ for RTX 50) and CMake >= 3.24. Run from anywhere.
set -euo pipefail
cd "$(dirname "$0")/.."
uv sync -q --group cuda
PY=$(uv run --no-sync python -c 'import sys; print(sys.executable)')
PYBIND=$(uv run --no-sync python -m pybind11 --cmakedir)
cmake -S cuda -B cuda/build -DCMAKE_BUILD_TYPE=Release -DPython_EXECUTABLE="$PY" -Dpybind11_DIR="$PYBIND"
cmake --build cuda/build -j
ls src/fleetline_matcher/_cuda_auction*.so
