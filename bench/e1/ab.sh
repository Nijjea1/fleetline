#!/usr/bin/env bash
# E1 before/after, interleaved: A, B, A, B, ... so slow drift of the machine (a laptop
# moving work to efficiency cores, heat) hits both configurations equally.
# Each run: fresh fleetd, short sweep, cool-down between steps and between runs.
#   bench/e1/ab.sh <reps> <rates>
set -euo pipefail
cd "$(dirname "$0")"
REPS="${1:-3}"; RATES="${2:-130000,150000,175000}"
for r in $(seq 1 "$REPS"); do
  FLEETD_ARGS="-ingest-workers 0" ./run.sh "ab_inline_$r" -rates "$RATES" -reps 1 -cooldown 20s
  sleep 60
  FLEETD_ARGS="-ingest-workers 4" ./run.sh "ab_workers4_$r" -rates "$RATES" -reps 1 -cooldown 20s
  sleep 60
done
