#!/usr/bin/env bash
# Build an OSRM road graph for NYC from an OpenStreetMap snapshot.
#
#   ml/osrm/build.sh            # uses the snapshot pinned below
#
# Steps (OSRM's Multi-Level Dijkstra pipeline):
#   1. osmium clips New York State to NYC's bounding box (smaller graph, less RAM).
#   2. osrm-extract turns roads into a routing graph using the "car" profile: which ways
#      cars may use, speeds by road type, turn restrictions, one-way streets.
#   3. osrm-partition + osrm-customize precompute a multi-level graph so a query only
#      explores a tiny part of the map. That's what makes 69k-pair matrices fast.
#
# The result is free-flow (no traffic) driving time. Traffic is what the ML model learns.
set -euo pipefail

SNAPSHOT="${SNAPSHOT:-new-york-261120}"   # pinned Geofabrik snapshot = reproducible graph
IMAGE="ghcr.io/project-osrm/osrm-backend:v5.27.1"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DIR="$ROOT/data/osrm"
mkdir -p "$DIR"

if [ ! -f "$DIR/$SNAPSHOT.osm.pbf" ]; then
  curl -fL -o "$DIR/$SNAPSHOT.osm.pbf" \
    "https://download.geofabrik.de/north-america/us/new-york/$SNAPSHOT.osm.pbf"
fi
if [ ! -f "$DIR/nyc.osm.pbf" ]; then
  osmium extract --bbox -74.30,40.48,-73.68,40.95 --strategy complete_ways \
    "$DIR/$SNAPSHOT.osm.pbf" -o "$DIR/nyc.osm.pbf"
fi

run() { docker run --rm -v "$DIR:/data" "$IMAGE" "$@"; }
run osrm-extract -p /opt/car.lua /data/nyc.osm.pbf
run osrm-partition /data/nyc.osrm
run osrm-customize /data/nyc.osrm
echo "built $DIR/nyc.osrm  (serve with ml/osrm/serve.sh)"
