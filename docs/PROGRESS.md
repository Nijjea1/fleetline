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
| 5. Greedy nearest-driver dispatch + metrics | ✅ 2026-11-20 (live) |
| 5b. Deterministic offline mode (`cmd/offline`) | ✅ 2026-11-21 |
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

## 2026-11-21
- Offline mode (D-011): `internal/offline`, `state.Mem`, `cmd/offline`. Leg-end heap in `world`.
- Determinism verified: identical SHA-1 for trips.csv and samples.csv across runs.
- Profiling: ring search with early stop (offline tests 4.52 s → 0.61 s); fixed start-of-day spawn stacking.
- 07:00–07:20 offline: p50 159 s / p90 579 s (live run 3: 153 / 536; real: 289 / 813), in 1.8 s vs 7 min live.
- First full day (`bench/runs/fullday_u075`): median 216 s vs real 212 s overall, but rush hours collapse (wild goose chase; 18:00 p50 1,482 s vs 207 s real). Runtime 6m42s, dominated by the rush-hour pending queue.
- Learning note 04.

### Next
1. Cap pickup ETA (real platforms do), then calibrate supply per hour so greedy reproduces the real hourly median waits.
2. Skip re-searching unchanged pending requests (cuts rush-hour runtime).
3. E1 load test on the live path.

## 2026-11-22
**Dispatch / simulation**
- Pickup-ETA cap (`-max-pickup`): ends the rush-hour wild goose chase. Sep 16 at utilization 0.75: no cap p90 1,370 s → cap 900 s p90 928 s (0.06% unserved) → cap 600 s p90 790 s (0.35% unserved). Real p90 550 s.
- Skip-unchanged retries: a waiting rider re-searches only when a new idle car appears nearby. Full day 4m18s → 2m37s.
- **Bug found by full-day A/B** (`-no-skip`): riders whose top-K cars were all claimed in one round were wrongly put to sleep. Fixed + regression test; full-day checksums now identical with and without the optimization (D-013). The earlier cap-600 numbers came from the buggy binary and were replaced.
- Per-hour supply calibration (`cmd/calibrate`): v1 oscillated; v2 damped + step-capped, still diverged after iteration 4; v3 adds **backtracking** (return to best, halve the step). Best mean per-hour error **0.191**, but only **10/18 hours** within ±15% (A5 not met). 17:00 collapses (+87%) while 19:00 and 22:00 run too fast (−62%, −51%).
- Held-out Sep 23 with Sep 16's settings: whole day p50 **217 s vs real 208 s**, p90 **597 vs 546 s**, unserved 0.07%. Per hour only 8/18 within ±25% (A6 not met). The day-level distribution transfers; hour-level doesn't.
- Diagnosis: supply is the only knob, waits are flat until a cliff (collapse), and most hours are *too fast* even at high utilization. Real waits include steps we don't simulate (driver accept/decline, re-dispatch). Decision pending: model an accept delay/decline rate (fitted, labeled as such) vs. relax A5/A6 to day-level targets.

**ML (`ml/`)**
- OSRM (NY + NJ, pinned snapshot) in Docker; 263×263 zone matrix; Sep 2026 hourly weather; dataset 13.8M / 1.9M / 4.1M rows split by date (D-012).
- E5 test MAE: OSRM 515.7 s → × hour-of-week 314.4 s → **LightGBM 236.5 s** → residual MLP 266.1 s (wider/longer MLP 268.1 s, no better). LightGBM is the ETA model to beat.
- E6 (Mac): ONNX export matches PyTorch (same test MAE). Idle machine: batch 1 ONNX Runtime CPU 0.009 ms vs M5 GPU 0.207 ms; batch 8,192 CPU 2.58 ms vs GPU 1.22 ms (crossover ~1k rows). A first run taken while training was using the CPU had overstated the GPU win 6× and was discarded. TensorRT script ready for the RTX 5060 (not yet run).
- Learning notes 05 (ETA) and 06 (cap, skip, calibration).

### Next
1. Finish calibration; validate on the held-out day Sep 23 (checklist A5/A6).
2. E1 load test on the live path.
3. Dashboard replay exporter.

## 2026-11-24
**Shipping**
- CI (GitHub Actions): gofmt/vet/race tests, Redpanda+Redis integration tests, Python lint+tests, dashboard build. Green on the first run.
- `make demo`: infra + fleetd + worldsim + summary in one command, verified from a cold start.
- Dashboard (Next.js 16 + deck.gl 9 + MapLibre): replay + live, deployed at **https://fleetline-sigma.vercel.app**. `cmd/export-replay` (5.8 MB brotli), fleetd `/live`. Caught a demo artifact: a 1 h warm-up showed a fake 6× win (D-015, note 08).
- Architecture diagram (`docs/architecture.svg`), README rewritten around measured results.
- Secrets scan (gitleaks): clean.

**E1 load test**
- `cmd/loadgen` (open loop, scrapes fleetd metrics). Baseline: **130k pings/s sustained (3/3)**, 140k not.
- Profiles → chunked Lua (latency at 130k halved, ceiling ≈ same) → concurrent apply workers (first full repetition on AC power: **175k sustained, p99 54 ms**).
- Backpressure: Redis frozen 10 s / down 5 s → lag peaks 511k / 244k, drains, **0 pings lost**. Found and fixed a data-loss bug (Redis errors dropped batches).
- Measurement traps found: rejected sends counted as sent, inherited consumer-group backlog, machine drift, and the laptop switching to battery + Low Power Mode at 23:03 (later runs discarded).

**Calibration (D-015):** Phase 1 target is day-level. Sep 16 calibrated: p50 206 vs 212 s, p90 586 vs 550 s. Held-out Sep 23: 217 vs 208, 597 vs 546.

### Next
1. On AC power: interleaved A/B (inline vs 4 workers) → final E1 numbers, README + resume bullet.
2. RTX 5060: `matcher/gpu_smoke/run.sh` (F1/F2), then TensorRT (E6).
3. Phase 2: accept/decline model, batched matching, CUDA matcher.

## 2026-11-25
**Final E1 A/B (on AC power; 3 interleaved pairs):** inline sustains 130k (3/3), 150k (2/3), peak 153k/s; 4 workers sustain 150k (3/3), 175k (2/3), peak **187.6k/s (+22%)**; p99 at 150k **242 → 46 ms**. Phase 1 done except E5 (repo public: user's call) and F (GPU machine).

### Resume bullets (every number from a committed result file)
- Built a real-time dispatch platform in Go on Kafka (Redpanda) and Redis that replays a full day of NYC Uber/Lyft demand (620k requests, 29k vehicles); profiled the ingest path and raised peak throughput **22% to 187k GPS pings/s** on one laptop while cutting p99 latency at 150k/s from **242 to 46 ms**.
- Proved zero data loss under failure: Redis frozen 10 s or killed 5 s, the backlog buffers in Kafka (up to 511k pings) and drains with every ping applied; the fault test found and fixed a silent batch-drop bug.
- Built a deterministic simulator calibrated against real trips (median wait **206 s vs real 212 s**; held-out day 217 vs 208 s) and a LightGBM ETA model cutting error **54% vs OSRM** (515.7 → 236.5 s MAE on 4.1M trips), plus a deck.gl dashboard rendering 29k live vehicles (5.8 MB replay, deployed on Vercel).

## 2026-11-25 (later): Phase 2 starts
- **Dashboard fixed + redesigned**: the black basemap was a real bug (MapLibre v6 worker 404 after bundling), not headless. Labels drawn above the cars; first-visit intro, clearer panel, playback bar, tooltips, phone layout. Deployed (prebuilt, see memory/notes: Vercel Hobby blocks co-authored commits).
- **E2/E3 batched matching**: `internal/assign` (exact sparse SSP solver, brute-force tested, ~0.1 ms/round) + `dispatch.Batched` with age weight 3 (D-016). Held-out Sep 23: mean wait **−18.6%**, p50 −18.4%, p90 −15.7%, empty drive −19.5%; unserved 0.07 → 0.10%.

### Resume bullet (E2)
- Cut average rider wait **18.6%** (p90 −15.7%) vs greedy dispatch with an exact batched min-cost assignment solver, measured by replaying a held-out day of **603k real NYC trips**; found and fixed rider starvation with an age-weighted objective.

### Next
1. E4: solver speed vs problem size: Go SSP vs SciPy Hungarian vs OR-Tools on cost matrices exported from real rounds; CUDA kernel on the RTX 5060.
2. Accept/decline model (closes the D-015 per-hour gap).
