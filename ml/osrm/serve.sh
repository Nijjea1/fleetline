#!/usr/bin/env bash
# Serve the NYC graph on http://localhost:5001 (5000 is often taken by macOS AirPlay).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
docker rm -f fleetline-osrm >/dev/null 2>&1 || true
docker run -d --name fleetline-osrm -p 5001:5000 -v "$ROOT/data/osrm:/data" \
  ghcr.io/project-osrm/osrm-backend:v5.27.1 \
  osrm-routed --algorithm mld --max-table-size 1000 /data/nyc.osrm
echo "OSRM on http://localhost:5001"
