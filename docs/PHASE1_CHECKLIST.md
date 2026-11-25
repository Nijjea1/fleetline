# Phase 1 Checklist: "Resume-ready"

Phase 1 is done when a recruiter can open a live demo in one click, and an interviewer can ask about any layer and get a measured answer. Every item below has a **done when** test. An item is checked only when its test passes, and the evidence (commit, file, number) is linked.

Legend: ✅ done · 🟡 in progress · ⬜ not started · 🖥️ needs the RTX 5060 machine

## A. Data and simulation

| # | Item | Done when | Status |
| --- | --- | --- | --- |
| A1 | Download real NYC demand | `fleetline-sim download` fetches HVFHV + zones; schema verified in `DATASETS.md` | ✅ |
| A2 | Reproducible replay day | `fleetline-sim prepare` → same seed gives a byte-identical parquet; cleaning counts printed | ✅ 635,294 trips |
| A3 | Travel model from data | detour factor, hourly speed, busy vehicles by hour in `travel_model_*.json` | ✅ |
| A4 | Deterministic offline simulator | Same seed → identical `trips.csv` checksum | ✅ |
| A5 | Supply calibrated per hour | Greedy's simulated median wait within ±15% of real in every hour 06:00–23:00 on the calibration day | ✅ day-level (D-015): p50 206 vs 212 s, p90 586 vs 550 s (`bench/runs/calibrated_sep16_cap600`); per hour 10/18 |
| A6 | Held-out validation day | Calibrated settings applied to a different weekday (Sep 23) stay within ±25% per hour; result committed | ✅ day-level: p50 217 vs 208 s, p90 597 vs 546 s (`bench/runs/heldout_sep23_cap600`); per hour 8/18 |

## B. Streaming core (Go)

| # | Item | Done when | Status |
| --- | --- | --- | --- |
| B1 | Kafka (Redpanda) + Redis via Docker Compose | `make up` healthy | ✅ |
| B2 | Ingest keyed by H3 cell | `vehicle.pings` keyed by H3 res-7; integration test round-trips 1,000 records | ✅ |
| B3 | Redis vehicle state with race guards | 6 integration tests pass (stale, duplicate, pre-ack, claim, nearest, batch) | ✅ |
| B4 | Greedy dispatch (+ robotaxi geofence, ETA cap) | unit tests pass; live run serves riders end to end | ✅ |
| B5 | Prometheus metrics + `/stats` | `curl :9100/metrics` shows ingest latency, round time, waits | ✅ |
| B6 | One command to run the live stack | `make demo` starts infra, fleetd and worldsim | ✅ `make demo`, verified from a cold start |

## C. Experiment E1: load test (the Phase 1 benchmark)

| # | Item | Done when | Status |
| --- | --- | --- | --- |
| C1 | Load generator | `bench/e1/` script ramps pings/s in steps without the dispatch loop's noise | ✅ `cmd/loadgen` (open loop) + `bench/e1/run.sh` |
| C2 | Sustainable throughput found | Table of offered vs. achieved pings/s and p50/p99 ingest latency; "knee" identified | ✅ baseline knee: 130k sustained 3/3, 140k not (`bench/e1/results/baseline*.json`) |
| C3 | Bottleneck profiled and fixed once | pprof + Redis stats show the bottleneck; one fix with before/after numbers | ✅ 2 profiles → chunked Lua (latency ½, ceiling ≈) → concurrent apply (ceiling ↑); final A/B 🟡 needs AC power |
| C4 | Backpressure behavior | Measured what happens when Redpanda/Redis slows (lag grows, no data loss) | ✅ Redis frozen 10 s / down 5 s: lag peaks 511k / 244k, drains, 0 lost (`bench/e1/backpressure.png`) |
| C5 | Spread, not one lucky run | ≥3 runs per config, spread reported | ✅ 3 runs per rate; interleaved A/B for before/after |
| C6 | Chart in README | PNG generated from `bench/e1/results/` by a script | ✅ `bench/e1/plot.py` → `e1.png`, `backpressure.png` |

## D. Dashboard (Next.js + deck.gl)

| # | Item | Done when | Status |
| --- | --- | --- | --- |
| D1 | Replay file exporter | Offline run → compact JSON/binary of vehicle positions + trips for a 20–30 min window, < 15 MB | ✅ `cmd/export-replay`: 5.8 MB brotli |
| D2 | Replay mode | Map plays cars (human vs. robotaxi colors), pickups, the robotaxi zone; play/pause/speed | ✅ |
| D3 | Live mode | Connects to fleetd (WebSocket/SSE) and shows the same view from Redis | ✅ fleetd `/live`, verified against `make demo` |
| D4 | Metrics panel | Wait distribution, served/unserved, real-vs-sim reference line | ✅ per-minute sim vs real median wait, counts, unserved |
| D5 | Deployed | Public Vercel URL opens with no setup | ✅ https://fleetline-sigma.vercel.app |

## E. Shipping

| # | Item | Done when | Status |
| --- | --- | --- | --- |
| E1 | CI | GitHub Actions runs Go + Python unit tests on every push | ✅ Go (vet, race), Kafka+Redis integration, Python, dashboard build |
| E2 | README | Pitch, architecture diagram, quick start, results with real numbers only | ✅ |
| E3 | Architecture diagram | `docs/architecture.svg` (source checked in) | ✅ `docs/architecture.svg` |
| E4 | Resume bullet (E1) | Bullet filled with the measured E1 numbers | 🟡 after the final A/B |
| E5 | Repo public | User decides; secrets scan clean first | ⬜ your call; gitleaks scan clean (102 commits); personal email is in pyproject.toml + commit authors |

## F. GPU machine smoke test 🖥️

| # | Item | Done when | Status |
| --- | --- | --- | --- |
| F1 | CUDA test kernel on the RTX 5060 | `nvcc` builds and runs a vector-add; output checked in `matcher/README.md` | ⬜ 🖥️ `matcher/gpu_smoke/run.sh` ready |
| F2 | PyTorch sees the GPU | `torch.cuda.is_available()` and a matmul on the 5060 | ⬜ 🖥️ same script |

## Not Phase 1 (tracked so they don't creep in)
Batching/Hungarian/CUDA matcher (Phase 2), OSRM + ETA model + TensorRT (Phase 3, started early in `ml/` at the user's request), AWS, Waymo.
