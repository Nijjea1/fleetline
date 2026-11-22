#!/usr/bin/env bash
# Build an OSRM road graph for NYC from an OpenStreetMap snapshot.
#
#   ml/osrm/build.sh            # uses the snapshot pinned below
#
# Steps (OSRM's Multi-Level Dijkstra pipeline):
#   1. osmium merges NY + NJ and clips to the NYC area (smaller graph, less RAM).
#   2. osrm-extract turns roads into a routing graph using the "car" profile: which ways
#      cars may use, speeds by road type, turn restrictions, one-way streets.
#   3. osrm-partition + osrm-customize precompute a multi-level graph so a query only
#      explores a tiny part of the map. That's what makes 69k-pair matrices fast.
#
# The result is free-flow (no traffic) driving time. Traffic is what the ML model learns.
set -euo pipefail

SNAPSHOT="${SNAPSHOT:-261120}"   # pinned Geofabrik snapshot date (YYMMDD) = reproducible graph
IMAGE="ghcr.io/project-osrm/osrm-backend:v5.27.1"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DIR="$ROOT/data/osrm"
mkdir -p "$DIR"

# New Jersey is included because Newark Airport (taxi zone 1) is in NJ, and some
# Staten Island <-> Manhattan routes are fastest through NJ. Without it, OSRM snapped EWR
# to a Staten Island road (EWR -> Midtown: 22.6 mi instead of ~16 mi).
for STATE in new-york new-jersey; do
  F="$STATE-$SNAPSHOT.osm.pbf"
  if [ ! -f "$DIR/$F" ]; then
    curl -fL -o "$DIR/$F" "https://download.geofabrik.de/north-america/us/$STATE/$F"
  fi
done
if [ ! -f "$DIR/nyc.osm.pbf" ]; then
  osmium merge "$DIR/new-york-$SNAPSHOT.osm.pbf" "$DIR/new-jersey-$SNAPSHOT.osm.pbf" \
    -o "$DIR/ny-nj.osm.pbf" --overwrite
  osmium extract --bbox -74.30,40.48,-73.68,40.95 --strategy complete_ways \
    "$DIR/ny-nj.osm.pbf" -o "$DIR/nyc.osm.pbf" --overwrite
fi

run() { docker run --rm -v "$DIR:/data" "$IMAGE" "$@"; }
run osrm-extract -p /opt/car.lua /data/nyc.osm.pbf
run osrm-partition /data/nyc.osrm
run osrm-customize /data/nyc.osrm
echo "built $DIR/nyc.osrm  (serve with ml/osrm/serve.sh)"
