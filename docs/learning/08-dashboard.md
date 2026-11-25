# 08 · The dashboard: drawing 29,000 moving cars at 60 fps

## What we built
- `cmd/export-replay`: runs the offline simulator and records a 20-minute window as three small files.
- `fleetd /live`: a snapshot of every vehicle in Redis, for live mode.
- `web/`: Next.js + deck.gl + MapLibre. Replay mode (the public demo, static files) and live mode (polls a running fleetd).

## Concepts

### Design the data, then the UI
The browser can't run the simulator, so we ship its output. Naively that's JSON like `{"id":"v123","lat":40.7581,"lon":-73.9855,"status":"idle"}` for 29k cars every 10 s for 20 minutes: ~250 MB. Our format:
- **Quantize coordinates to uint16** inside the NYC bounding box: 0.47° of latitude / 65,535 steps ≈ 0.8 m. Nobody can see a 1 m error on a city map. 4 bytes per position instead of ~40 characters.
- **No IDs in frames.** Vehicle *i* is always slot *i*; `meta.json` lists each slot's kind once.
- **Columnar per frame** (all lats, then all lons, then all statuses), which compresses far better than interleaved records because similar numbers sit together.

Result: 17.8 MB raw, **5.8 MB with brotli**, which Vercel serves automatically.

### Interpolate instead of storing more frames
Frames are 10 simulated seconds apart. At 30× playback that's 3 frames per wall second, so without interpolation cars would jump. The browser draws every frame (60 fps) by linearly blending positions between the two surrounding snapshots. One edge case: a car that logs on between frames has no "before" position, so it must not slide in from (0, 0). The code checks for that.

### Binary attributes: why it stays fast
The obvious deck.gl code passes an array of 29k JavaScript objects and accessor functions (`getPosition: d => [d.lon, d.lat]`). deck.gl then calls those functions 29k times per update and builds GPU buffers from them. Instead we fill one `Float32Array` of positions and one `Uint8Array` of colors and hand them over as **binary attributes**: they go to the GPU as-is. The per-frame work is a single tight loop over typed arrays.

### Static export
`output: "export"` makes the whole site plain files: HTML, JS, and the recording. No server, so it can't go down, costs nothing, and works on any host. Live mode is the exception: it talks to a fleetd you run locally (`make demo`).

### A result too good to be true (again)
The first export used a 1-hour warm-up. The dashboard showed Fleetline's median wait at 0:56 vs Uber/Lyft's 5:38: a 6× "win". It was an artifact: the fleet started mostly idle and hadn't reached steady state by 08:00. Warming up from 01:00, like the calibration, gives 3:06 vs 4:40: still faster than reality, by the known gap (no accept/decline time; D-015), which the dashboard states next to the chart. **A demo is a claim. Check it like a benchmark.**

### Verifying a WebGL page without a human
Headless Chrome with a software GPU (`--use-angle=swiftshader`) can screenshot a deck.gl page. Base-map tiles don't render in that mode, but the data layers do, which is what we needed to check. `?mode=live` exists partly so live mode can be screenshotted without clicking.

## Interview questions this answers
- "How would you render tens of thousands of moving points in a browser?"
- "How do you make a demo cheap to host and impossible to take down?"
- "Your simulator beats Uber by 6×. Why don't you believe it?"
