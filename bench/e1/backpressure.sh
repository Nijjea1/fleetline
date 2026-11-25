#!/usr/bin/env bash
# E1 / C4: what happens when a dependency stalls? Offer a steady load, freeze Redis
# (docker pause) for 10 s mid-step, and record sent / applied / lag every second.
# Pass = lag grows while Redis is frozen, drains afterwards, and applied == sent at the
# end (nothing lost). Results: bench/e1/results/backpressure.json
set -euo pipefail
cd "$(dirname "$0")"
RATE="${1:-100000}"
( sleep 45; echo "pausing redis"; docker pause fleetline-redis-1 >/dev/null
  sleep 10; docker unpause fleetline-redis-1 >/dev/null; echo "unpaused redis" ) &
./run.sh backpressure -rates "$RATE" -reps 1 -step 60s -timeline
