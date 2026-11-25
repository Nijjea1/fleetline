#!/usr/bin/env bash
# D-017 check + E2b, on held-out Sep 23 only, with the offer model chosen on Sep 16
# (10 s answer delay, decline 0.10 + 0.04 per pickup minute) and its re-fitted supply.
#   validation: does greedy + offers reproduce real per-hour waits better than without?
#   E2b       : does batched still beat greedy when drivers decline? (seeds 7-9)
# Baseline (no offers) runs are reused from bench/e2/runs. JOBS=2 keeps the laptop cooler.
set -euo pipefail
cd "$(dirname "$0")"
JOBS="${JOBS:-2}"
(cd ../../core && go build -o bin/offline ./cmd/offline)
run() {
  local pol=$1 seed=$2 out="runs/09-23_${1}_offers_s${2}"
  [ -f "$out/summary.json" ] && { echo "skip $out"; return; }
  ../../core/bin/offline -replay ../../data/derived/day_2026-09-23_seed7.parquet \
    -model ../../data/derived/travel_model_2026-09-23.json \
    -from 01:00 -to 23:59 -warmup 60m -max-pickup 600s \
    -supply data -supply-file calib/d10_b10_m04.json \
    -offer-delay 10s -decline-base 0.10 -decline-per-min 0.04 \
    -policy "$pol" -window 1s -seed "$seed" -out "$out" 2>&1 | grep -oE "msg=done.*" | sed "s#^#$out #"
}
export -f run
for p in greedy batched; do for s in 7 8 9; do echo "$p $s"; done; done | xargs -P "$JOBS" -L 1 bash -c 'run "$@"' _
