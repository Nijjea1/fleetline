#!/usr/bin/env bash
# D-017: fit the driver offer model on Sep 16 only. For each candidate (answer delay,
# base decline, decline per pickup minute), re-fit hourly supply with cmd/calibrate and
# record how well the greedy baseline then reproduces real per-hour median waits.
# The baseline without offers is bench/calibration/sep16_cap600.json.
set -euo pipefail
cd "$(dirname "$0")/../../core"
go build -o bin/calibrate ./cmd/calibrate
cands="d10_b10_m04 10s 0.10 0.04
d20_b10_m04 20s 0.10 0.04
d10_b20_m04 10s 0.20 0.04
d20_b20_m04 20s 0.20 0.04"
echo "$cands" | xargs -P 4 -L 1 bash -c 'out=../bench/offers/calib/$0.json; [ -f "$out" ] && exit 0;
  bin/calibrate -max-pickup 600s -iters 12 -offer-delay $1 -decline-base $2 -decline-per-min $3 -out "$out" 2>&1 | grep -E "wrote|iteration" | sed "s#^#$0 #"'
