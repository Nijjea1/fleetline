# Decision Log

Short architecture decision records. Each entry: what we chose, what we rejected, and why. Interviewers love "why did you do X instead of Y" — this file is the answer key.

---

## D-001 · Demand data: NYC High-Volume FHV over yellow taxi

- **Date:** 2026-11-20
- **Chose:** NYC TLC High-Volume FHV (Uber/Lyft) trip records.
- **Rejected:** Yellow taxi records (street hails, no request time); Chicago TNP (15-minute time rounding).
- **Why:** HVFHV has `request_datetime` and `on_scene_datetime`, so we get the real production wait time as a reference line for every dispatch experiment. Details in `DATASETS.md`.

## D-002 · Dashboard: live mode + replay mode

- **Date:** 2026-11-20
- **Chose:** The dashboard has two modes. *Live* connects to the running backend (local now, AWS in Phase 3). *Replay* plays a recorded simulation file in the browser and is what the public Vercel link shows.
- **Rejected:** Hosting the backend just so the demo link works.
- **Why:** Vercel cannot run Kafka, Redis or a long-lived Go service, and an always-on cloud backend costs money. Replay keeps the one-click demo free and always up; live mode proves the real system works.

## D-003 · Zone IDs → coordinates by seeded sampling

- **Date:** 2026-11-20
- **Chose:** For each trip, sample a uniform random point inside its taxi-zone polygon using a fixed seed.
- **Rejected:** Zone centroids (every trip in a zone stacks on one point, which makes nearest-driver distances fake); older GPS data (TLC republished it with zone IDs only — verified).
- **Why:** Keeps distances realistic inside a zone while staying reproducible: same seed → same points → same experiment.

## D-004 · Local infrastructure via Docker Compose

- **Date:** 2026-11-20
- **Chose:** Docker Desktop, with Redpanda (Kafka-compatible) and Redis in `infra/docker-compose.yml`.
- **Rejected:** Real Apache Kafka locally (needs more memory and setup).
- **Why:** Redpanda speaks the Kafka protocol, so the Go code is unchanged when we move to AWS MSK in Phase 3.

## D-005 · Python tooling: uv + Python 3.12

- **Date:** 2026-11-20
- **Chose:** `uv` to manage Python versions and dependencies; Python 3.12.
- **Rejected:** System Python 3.9 + pip.
- **Why:** The system Python is old, and `uv` gives a lockfile, so every machine (Mac and the RTX 5060 box) installs identical versions.

## D-006 · "World" vs. "system": two Go programs

- **Date:** 2026-11-20
- **Chose:** `worldsim` plays the city (replays rider requests, drives vehicles, emits GPS pings and trip events). `fleetd` is the production system (ingests pings, keeps state in Redis, dispatches). They only talk through Kafka.
- **Rejected:** One program that both simulates and dispatches; writing the world simulator in Python.
- **Why:** The dispatcher must not cheat by reading simulator memory: it sees exactly what a real dispatcher sees, late and out of order. Go because a 10k-vehicle fleet pinging every 4 s at 10× speed is ~25k events/s, too much for a Python loop. Python stays for data prep.

## D-007 · Event time, not wall-clock time

- **Date:** 2026-11-20
- **Chose:** Every event carries its simulated timestamp. `fleetd`'s clock is a *watermark*: the latest event time it has seen.
- **Rejected:** Each process computing sim time from its own wall clock.
- **Why:** Replays run at 1×, 10× or 100× speed. With event time the dispatcher's decisions don't depend on how fast the replay runs or on clock drift between processes (the Flink/Kafka Streams model).

## D-008 · Pings keyed by H3 cell + a sequence guard in Redis

- **Date:** 2026-11-20
- **Chose:** Pings are keyed by the vehicle's H3 res-7 cell (as the scope says). Redis writes go through a Lua script that (a) drops pings older than the stored one and (b) never lets a ping overwrite a dispatcher assignment the vehicle hasn't acknowledged yet (`ping.seq < pending_seq`).
- **Rejected:** Keying pings by vehicle ID (keeps per-vehicle order, but loses geographic partitioning).
- **Why:** When a vehicle crosses a cell boundary, its pings switch partitions, so order is no longer guaranteed. And a ping sent just before the vehicle received an assignment would mark it "idle" again, causing a double assignment. Both races are handled atomically in Redis.

## D-009 · JSON on the wire first

- **Date:** 2026-11-20
- **Chose:** JSON messages.
- **Rejected (for now):** Protobuf.
- **Why:** JSON is readable in Redpanda Console while learning. If E1 shows serialization is a bottleneck, switching to protobuf becomes a measured improvement instead of a guess.

## D-010 · Simulation validity: warm-up, leak-free spawn, data-sized fleet

- **Date:** 2026-11-20
- **Chose:**
  1. **Fleet size from data:** `fleet = real busy vehicles at the window's start hour / utilization` (utilization default 0.75, a stated assumption, swept in experiments).
  2. **Spawn idle cars at the previous hour's dropoff points.**
  3. **45-minute warm-up** before the measured window; warm-up requests are dispatched normally but flagged and excluded from stats.
  4. **Drain** for patience + 30 min after the last request so every assigned rider is counted.
- **Rejected:** A guessed fleet size; spawning at pickup points of the replayed requests; measuring from a cold start.
- **Why:** The first data-sized run reported a **0.01 s median wait**, which is impossible. Root causes: cars were spawned at the pickup points of the requests about to be replayed (a *data leak*: supply placed using future demand); every car started idle although ~16k real cars were mid-trip at 7:30; and a text edit meant to extend the drain had silently not applied. Lesson: a result that is too good is a bug until proven otherwise.

## D-011 · Offline mode for policy experiments; live mode for systems experiments

- **Date:** 2026-11-21
- **Chose:** `cmd/offline` runs `world` + a dispatch `Policy` + an in-memory pool (`state.Mem`) in one goroutine on a fixed simulated clock. The dispatcher sees current positions (no ping delay). E2/E3 (and every policy comparison) run here. E1 (throughput, latency, backpressure) and the dashboard use the live path.
- **Rejected:** Measuring policy quality on the live system.
- **Why:** Live run 3 showed fleetd falling 7.3 s behind at ~82k pings/s, so live waits partly measure laptop speed. Offline runs are byte-identical for the same seed (verified with checksums) and ~200× faster (07:00–07:20 in 1.8 s vs 7 min live). The policy code is shared, so nothing is reimplemented.
- **Known gap:** Offline has no information delay. If a policy's ranking could flip under delay, we test that live.

## D-012 · ETA at taxi-zone resolution, with OSRM on NY + NJ roads

- **Date:** 2026-11-22
- **Chose:** Predict trip time from (pickup zone, dropoff zone, time, weather), with OSRM free-flow time between zone representative points as the routing baseline. The road graph is a pinned Geofabrik snapshot (261120) of New York **and** New Jersey, clipped to the NYC bounding box.
- **Rejected:** Point-level routing (the public HVFHV data has no coordinates); NY-only roads; the `latest` Geofabrik file.
- **Why:** Zones are the finest resolution the real labels have, so any finer model would be fitting noise we invented. NJ roads are needed because Newark Airport is in NJ: without them OSRM snapped EWR to Staten Island (22.6 mi to Midtown instead of 16.9). A pinned snapshot makes the graph, and every number built on it, reproducible.
- **Consequence:** Some error is irreducible (where inside the zone the rider stood), so absolute MAE isn't comparable to point-level published systems; the comparison that matters is between our own baselines and models on the same split.

## D-013 · Optimizations must be proven output-identical with an in-binary switch

- **Date:** 2026-11-22
- **Chose:** Every performance shortcut in the simulator ships with a flag that turns it off (`-no-skip`). It's accepted only if a **full-day** run gives the same trips.csv checksum both ways.
- **Rejected:** Comparing old vs. new binaries; validating on a short window only.
- **Why:** A short-window A/B passed, but the full-day A/B exposed a real bug (top-K truncation, see learning note 06). Comparing two binaries had given a false alarm: an unrelated refactor shifted the driver target by one car in 6 of 1,481 minutes, which changes the whole chaotic trajectory.

## D-014 · Ingest: chunked Lua + concurrent apply workers; pings may be dropped on a crash

- **Date:** 2026-11-24
- **Chose:** fleetd applies pings with one Redis script call per 256-ping chunk, from 4 concurrent workers fed by a bounded queue (`-ingest-workers 4`). The old paths stay behind flags (`-per-ping-script`, `-ingest-workers 0`) for A/B runs.
- **Rejected (for now):** sharding Redis or keeping vehicle state in fleetd's memory. Both are the next steps if one Redis core becomes the limit again.
- **Why:** E1 profiles (see learning note 07). Chunking cut latency at 130k pings/s by ~half but barely moved the ceiling; Redis was only ~82% busy because fleetd decoded and waited serially. Overlapping decode with Redis writes raised the sustainable rate (numbers in `bench/e1/`).
- **Trade-off:** offsets are auto-committed when polled, so after a crash up to 4 queued batches (~20k pings) could be lost. Acceptable for pings only: every vehicle pings every 4 s, so lost state heals within seconds, and cross-batch reordering is already handled by the per-vehicle timestamp guard (D-008). Ride requests and trip events are still handled synchronously.

## D-015 · Phase 1 calibration target: whole-day wait distribution, not every hour

- **Date:** 2026-11-24
- **Chose:** A5/A6 pass on the **day-level** wait distribution (median and p90 within 15% on the held-out day). Per-hour errors are reported alongside, not hidden.
- **Rejected (for Phase 1):** adding a fitted driver accept/decline model to chase per-hour targets.
- **Why:** With supply as the only knob, per-hour calibration plateaus at 10/18 hours (best mean error 0.191) because waits are flat until a cliff and hours are coupled. Most hours are *too fast*, consistent with real waits including accept/decline time the simulator doesn't model. That model has no ground truth in the public data, so it would be a fitted assumption; it's the first item for Phase 2. The day-level distribution already transfers to an unseen day (Sep 23: p50 217 vs 208 s, p90 597 vs 546 s).
- **Consequence:** policy comparisons are valid *relative to each other* on the same simulator; absolute wait numbers are not claimed to match reality hour by hour.

## D-016 · Batched matching: exact sparse solver in Go, with an age weight

- **Date:** 2026-11-25
- **Chose:** `dispatch.Batched` solves each round's riders × (top-K candidate cars) as one min-cost assignment with `internal/assign`: successive shortest paths with Dijkstra and potentials, on the sparse edge list. Objective: match as many riders as possible, then minimize Σ (pickup ETA + 1 s per second the rider has *not* waited relative to the oldest rider), i.e. `-age-weight 3`.
- **Rejected:** calling SciPy / OR-Tools from the simulator (a process boundary every simulated second, and a dense 1,000 × 29,000 matrix where each rider has 8–16 real options); age weights 0 and 1.
- **Why:** The solver is exact (matches brute force on 2,000 random instances) and fast enough to run every round (~0.1 ms for 300 riders × 8 cars). With age weight 0, riders competing for the last nearby car could lose round after round: unserved went from 0.03% (greedy) to 0.24% on 07:00–08:00. Weight 1 fixed that hour but not the full day: unserved 0.10% (greedy) → 0.27%, concentrated in shortage hours (7 AM: 9 → 255). When not every rider can be matched, weight 1 lets a new rider 50 s from a car beat one who waited 8 min but is 580 s away; with pickups up to 600 s and patience 600 s, the weight must be well above 1 for age to win. Tuned on Sep 16 only (full day, 1 s window, seed 7): weight 2 / 3 / 5 / 10 → unserved 0.16 / 0.12 / 0.12 / 0.11%, p90 488 / 487 / 497 / 510 s. Chose 3. The sweep over Sep 23 was stopped before any batched result, so the held-out day was run once, with 3.
- **Consequence:** SciPy, OR-Tools and the CUDA solver belong in E4 (solver speed on cost matrices exported from real rounds), not in the simulator loop. Same candidate search, radius and pickup cap as greedy, so E2 isolates the matching step.

## D-017 · Driver offer model: kept as a robustness check, off by default

- **Date:** 2026-11-25
- **Built:** `offline.Offers`. A human driver answers each offer after a delay and declines with probability `base + per_min × pickup minutes`, because acceptance falls with pickup time ([Ashkrof et al. 2022](https://arxiv.org/abs/2107.07864), [arXiv 2402.01650](https://arxiv.org/abs/2402.01650)). Robotaxis accept instantly. With the model off, output is byte-identical to before.
- **Fit (Sep 16 only, `bench/offers/calibrate.sh`):** 4 candidates, each with supply re-fitted. Every candidate fit Sep 16's hourly medians better than no offers (mean error 0.138–0.183 vs 0.191; 14–17 vs 13 of 23 hours within 15%). Chose 10 s delay, 0.10 + 0.04/min (best: 0.138, 17/23), which gives a 27% decline rate.
- **Held-out check (Sep 23, 3 seeds):** the per-hour fit did **not** transfer: mean error 38.4% → 35.3%, but hours within 15% fell 7 → 4. Day level is unchanged (p50 206 vs real 208 s; p90 608 vs 546 s). The Sep 16 gain came from re-fitting 24 hourly supply values together with the offer settings, i.e. overfitting one day. Hourly supply differs between days, and that's the real limit.
- **Chose:** don't claim the model makes the simulator more realistic. Keep it off by default and use it as a stress test of the policy result (E2b).
- **E2b result:** with 24–27% of offers declined, batched still beats greedy on held-out Sep 23. Mean wait −16.9% (vs −18.6% without offers), p90 −16.9%, empty drive −18.1%. Batched also gets fewer declines (24.1% vs 27.4%) because its pickups are shorter.

## D-018 · GPU assignment: auction on a square reformulation, integer costs

- **Date:** 2026-11-26
- **Chose:** forward auction with ε-scaling (Bertsekas), Jacobi style (all unassigned persons bid at once), on an equivalent **square** problem: persons = riders + one "slack" person per car, objects = cars + one "unmatched" object per rider. Slack person j can take car j (cost 0) or the unmatched object of any rider with an edge to car j (cost 0). Costs are rounded to whole ms (as OR-Tools does) and multiplied by N + 1, so the last phase at ε = 1 is exact for the rounded costs.
- **Rejected:** the plain rectangular version (riders + private dummy, cars left over). Forward auction is only provably optimal there if every car left unassigned ends with price 0, which ε-scaling breaks (a car priced up in an early phase can end idle and still be priced). Fixing that needs a reverse-auction pass. The square version keeps the textbook guarantee and only doubles the edge count. Also rejected: packing (bid, rider) into one 64-bit word for `atomicMax`. Scaled bids need ~49 bits and ids ~19, so ties are resolved in a second pass with `atomicMin` on the rider id instead.
- **Why:** the GPU column of E4 must be exact or it's not comparable; the bench verifies every total against Go's SSP.
