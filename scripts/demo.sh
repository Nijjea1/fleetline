#!/usr/bin/env bash
# One command to see the live system: Redpanda + Redis, fleetd (dispatch), worldsim (the city).
#   make demo                      # 07:00-07:20 at 10x
#   FROM=17:00 TO=17:30 SPEED=15 make demo
# Watch: http://localhost:9100/stats (dispatch summary), :9100/metrics (Prometheus),
#        http://localhost:8080 (Redpanda Console: topics, partitions, consumer lag).
set -euo pipefail
cd "$(dirname "$0")/.."
FROM="${FROM:-07:00}"; TO="${TO:-07:20}"; SPEED="${SPEED:-10}"

for f in data/derived/day_2026-09-16_seed7.parquet data/derived/travel_model_2026-09-16.json; do
  [ -f "$f" ] || { echo "missing $f: run 'cd sim && uv run fleetline-sim download && uv run fleetline-sim prepare && uv run fleetline-sim calibrate'"; exit 1; }
done

docker compose -f infra/docker-compose.yml up -d --wait
(cd core && go build -o bin/fleetd ./cmd/fleetd && go build -o bin/worldsim ./cmd/worldsim)

mkdir -p bench/runs/demo
(cd core && exec ./bin/fleetd) > bench/runs/demo/fleetd.log 2>&1 &
FLEETD=$!
trap 'kill $FLEETD 2>/dev/null; wait $FLEETD 2>/dev/null; echo "fleetd stopped; infra still up (make down)"' EXIT

for i in $(seq 1 30); do curl -sf localhost:9100/stats >/dev/null && break; sleep 1; done
echo "fleetd up: http://localhost:9100/stats   console: http://localhost:8080"
(cd core && ./bin/worldsim -from "$FROM" -to "$TO" -speed "$SPEED")
sleep 3
echo; echo "== dispatch summary"; curl -s localhost:9100/stats; echo
