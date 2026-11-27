#!/usr/bin/env bash
# E4 GPU profiles: where cuda_auction's time goes at 30k and 100k riders (seed 0).
#   nsys: kernel vs copy vs API/launch time            -> nsys_<n>.txt
#   ncu:  occupancy, memory and compute throughput of one auction phase -> ncu_<n>.txt
# Needs the solver built (matcher/cuda/build.sh) and data/derived (sim prepare).
set -euo pipefail
cd "$(dirname "$0")"
HERE=$(pwd)
RUN=(uv run --no-sync --project "$HERE/../../../matcher" python "$HERE/run_once.py")
# The first solve call is a warm-up (CUDA context, module load) that makes a few
# launches of its own; skip those, then 6 phases of the real solve: a mid-run phase.
WARM=$("${RUN[@]}" --warm)
# cuda_auction's own warm-up (solvers.py) makes the same number of launches again.
SKIP=$((2 * WARM + 6))
for n in 30000 100000; do
  nsys profile -t cuda,osrt -o "/tmp/e4_$n" --force-overwrite true "${RUN[@]}" "$n" > "/tmp/nsys_run_$n.txt" 2>&1
  {
    echo "# nsys: cuda_auction, n=$n seed=0, $(nvidia-smi --query-gpu=name,driver_version --format=csv,noheader), $(nsys --version)"
    grep "^n=" "/tmp/nsys_run_$n.txt"
    nsys stats -q -r cuda_gpu_kern_sum,cuda_gpu_mem_time_sum,cuda_api_sum -f column "/tmp/e4_$n.nsys-rep"
  } > "nsys_$n.txt"
  {
    echo "# ncu: one mid-run phase_kernel launch (skip $SKIP), n=$n seed=0"
    ncu --kernel-name phase_kernel --launch-skip "$SKIP" --launch-count 1 \
      --section SpeedOfLight --section Occupancy --section LaunchStats --section MemoryWorkloadAnalysis \
      "${RUN[@]}" "$n" 2>&1
  } > "ncu_$n.txt" || true
done
ls -la
