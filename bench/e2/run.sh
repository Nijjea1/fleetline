#!/usr/bin/env bash
# E2 (does batching beat greedy?) and E3 (how long should the batch window be?).
# Full simulated days (01:00-23:59, 1 h warm-up), calibrated supply, 10-minute pickup cap.
# Same day, same seed, same supply: only -policy / -window change.
#   sweep : both policies x windows 1,2,5,10 s, seed 7, on Sep 16 and held-out Sep 23
#   seeds : 1 s window, seeds 8 and 9, to show run-to-run spread
# Results: bench/e2/runs/<day>_<policy>_w<window>_s<seed>/summary.json
set -euo pipefail
cd "$(dirname "$0")"
JOBS="${JOBS:-4}"
(cd ../../core && go build -o bin/offline ./cmd/offline)

jobs() {
  for day in 2026-09-16 2026-09-23; do
    for pol in greedy batched; do
      for w in 1 2 5 10; do echo "$day $pol $w 7"; done
      for s in 8 9; do echo "$day $pol 1 $s"; done
    done
  done
}

run() {
  local day=$1 pol=$2 w=$3 seed=$4
  local out="runs/${day#2026-}_${pol}_w${w}_s${seed}"
  [ -f "$out/summary.json" ] && { echo "skip $out"; return; }
  ../../core/bin/offline -replay "../../data/derived/day_${day}_seed7.parquet" \
    -model "../../data/derived/travel_model_${day}.json" \
    -from 01:00 -to 23:59 -warmup 60m -max-pickup 600s \
    -supply data -supply-file ../calibration/sep16_cap600.json \
    -policy "$pol" -window "${w}s" -seed "$seed" -out "$out" 2>&1 | grep -oE "msg=done.*" | sed "s#^#$out #"
}
export -f run
jobs | xargs -P "$JOBS" -L 1 bash -c 'run "$@"' _
