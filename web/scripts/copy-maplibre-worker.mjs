// MapLibre v6 loads its tile-parsing worker from a URL next to its own script. Bundlers
// move the script, so the worker 404s and the basemap never draws. Ship the worker as a
// static file (same version as the installed package) and point MapLibre at it.
import { copyFileSync } from "node:fs";
copyFileSync("node_modules/maplibre-gl/dist/maplibre-gl-worker.mjs", "public/maplibre-gl-worker.mjs");
