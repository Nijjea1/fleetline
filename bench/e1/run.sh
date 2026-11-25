#!/usr/bin/env bash
# E1 load test: fresh fleetd, then loadgen steps. Usage: bench/e1/run.sh <label> [loadgen flags]
# Needs `make up`. Results: bench/e1/results/<label>.json, fleetd log next to it.
set -euo pipefail
cd "$(dirname "$0")/../../core"
LABEL="${1:?label}"; shift
go build -o bin/fleetd ./cmd/fleetd && go build -o bin/loadgen ./cmd/loadgen
mkdir -p ../bench/e1/results
./bin/fleetd ${FLEETD_ARGS:-} > "../bench/e1/results/$LABEL.fleetd.log" 2>&1 &
FLEETD=$!
trap 'kill $FLEETD 2>/dev/null || true' EXIT
for i in $(seq 1 30); do curl -sf localhost:9100/stats >/dev/null && break; sleep 1; done
./bin/loadgen -label "$LABEL" "$@"
