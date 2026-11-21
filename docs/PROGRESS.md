# Progress Log

A running record of what was built, when, and what is next. Dates are real work dates.

## Phase 1 · Core

| Step | Status |
| --- | --- |
| 0. CUDA test kernel + PyTorch on RTX 5060 | ⬜ To do on the GPU machine |
| 1. Repo skeleton, docs, docker compose | ✅ 2026-11-20 |
| 2. Download data, build replay day | ✅ 2026-11-20 |
| 3. Event replayer → Kafka (`worldsim`) | ✅ 2026-11-20 |
| 4. Go ingest, H3-keyed Kafka, Redis state (`fleetd`) | ✅ 2026-11-20 |
| 5. Greedy nearest-driver dispatch + metrics | ✅ 2026-11-20 (live); offline mode next |
| 6. deck.gl dashboard (live + replay), Vercel | ⬜ |
| 7. E1 load test, README + architecture diagram | ⬜ |

## 2026-11-20
- Installed Go 1.27.4, gh 2.105.0, uv 0.13.9. Docker Desktop needs an admin password, so the user installs it.
- Researched datasets; chose NYC High-Volume FHV (Uber/Lyft) for its request and on-scene timestamps (D-001). Verified that TLC's republished 2015 files no longer contain lat/lon.
- Monorepo scaffold, decision log, dataset doc, docker compose (Redpanda, Console, Redis), Makefile.
- `sim/`: downloader, seeded zone sampler (4 tests), replay-day builder. Replay day 2026-09-16 → 635,294 trips, deterministic.
- Learning notes 01 and 02.

- Docker Desktop and gh set up; private repo pushed to GitHub.
- `fleetline-sim calibrate`: detour factor 1.333, speed by hour (8.6–17.2 mph), busy vehicles by hour (1,908–20,564).
- Go core: model, geo (+ robotaxi geofence), bus, state (Lua guards, 6 race tests), replay, world, dispatch (greedy), metrics, `worldsim`, `fleetd`.

### Live runs (all on the M5 MacBook, Docker Desktop, 2026-11-20)

| Run | Setup | Result | Verdict |
| --- | --- | --- | --- |
| 1 | 07:00–07:30, guessed fleet 8,000, 30× | 21.5% riders gave up; dispatch round mean 146 ms | Fleet far too small (real busy 16,441) |
| 2 | 07:00–07:20, fleet 21,921 from data, 10× | median wait **0.01 s** | **Invalid**: data leak + cold start + drain bug (D-010) |
| 3 | Same + 45 min warm-up, leak-free spawn, 15× | 11,208 riders served, 0 expired; wait mean 231 s, p50 153 s, p90 536 s | Plausible, but see ingest lag below |

Reference for the same 07:00–07:20 window, real Uber/Lyft: **mean 381 s, p50 289 s, p90 813 s.** Our simulated greedy is faster than reality. Expected: utilization 0.75 is an assumption, and real waits include driver accept and decline time we don't model. Supply will be calibrated so greedy reproduces the real distribution before E2.

**Run 3 ingest finding (feeds E1):** 34.5M pings in 7 wall-minutes (~82k pings/s offered). fleetd's mean batch age at write time was **7.3 s**, so it fell behind. ~1.0M pings arrived stale and ~0.5M were position-only (the D-008 guards working as designed under load). The dispatch rounds also slowed (408 rounds, mean 122 ms) because Redis was saturated running a Lua script per ping. So at 15× with a 22k fleet, the laptop is past single-node capacity. Policy-quality numbers must not depend on that (→ offline mode, below).

### Next
- **Offline mode**: run `world` + `dispatch` in one deterministic process with an in-memory pool (no Kafka/Redis timing), for E2/E3 policy comparisons. The live path stays for E1 and the demo.
- **E1 load test**: find the sustainable pings/s and the knee; then profile (Lua per ping vs. JSON decode vs. single consumer).
- Dashboard (live + replay).
