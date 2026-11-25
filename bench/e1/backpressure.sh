#!/usr/bin/env bash
# E1 / C4: what happens when a dependency stalls? Offer a steady load, freeze Redis
# (docker pause) for 10 s mid-step, and record sent / applied / lag every second.
# Pass = lag grows while Redis is frozen, drains afterwards, and applied == sent at the
# end (nothing lost). Results: bench/e1/results/backpressure.json
set -euo pipefail
cd "$(dirname "$0")"
# MODE=pause: Redis hangs (writes block).  MODE=stop: Redis is down (writes fail and are
# retried); Redis has no persistence here, so it comes back empty and pings rebuild it.
RATE="${1:-100000}"; MODE="${MODE:-pause}"
if [ "$MODE" = pause ]; then
  ( sleep 45; echo "pausing redis"; docker pause fleetline-redis-1 >/dev/null
    sleep 10; docker unpause fleetline-redis-1 >/dev/null; echo "unpaused redis" ) &
else
  ( sleep 45; echo "stopping redis"; docker stop -t 0 fleetline-redis-1 >/dev/null
    sleep 5; docker start fleetline-redis-1 >/dev/null; echo "started redis" ) &
fi
./run.sh "backpressure_$MODE" -rates "$RATE" -reps 1 -step 60s -timeline
