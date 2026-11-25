# web/: dashboard (Next.js + deck.gl)

Two modes (D-002):

| Mode | What it shows | Needs |
| --- | --- | --- |
| **Replay** (default, the public demo) | A recorded 20-minute window of a real NYC day: every vehicle (idle / to pickup / on trip), riders waiting, pickups as they happen, the robotaxi zone, and the median wait per minute next to what real Uber/Lyft riders waited for the same requests | Nothing: static files |
| **Live** | The running system: polls fleetd's `/live` (vehicle state from Redis) once a second | `make demo` running locally |

## Run

```bash
# 1. record a replay (from the repo root; needs the prepared day, see sim/)
cd core && go run ./cmd/export-replay -from 08:00 -to 08:20   # -> web/public/replay/
# 2. dashboard
cd ../web && npm install && npm run dev                        # http://localhost:3000
```

Live mode: `make demo` in another terminal, then switch the toggle. Another fleetd address: `?fleetd=http://host:9100`.

## Deploy

`npm run build` writes a static site to `out/` (Next.js `output: "export"`), so any static host works. On Vercel: import the repo, set the root directory to `web/`, deploy. The recorded run lives in `public/replay/` and ships with the site.

## How it stays fast

- `export-replay` stores positions as uint16 inside the NYC bounding box (~1 m precision) and writes columns per frame, ~5 bytes per vehicle per 10 simulated seconds.
- In the browser, each animation frame interpolates all ~22k vehicles into one `Float32Array` and hands it to deck.gl as a binary attribute: no per-vehicle JavaScript objects, so it stays at 60 fps.
- Basemap: CARTO Dark Matter via MapLibre (no API key).
