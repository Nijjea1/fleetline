# 04 · Offline mode: a deterministic experiment engine

## What we built
- `internal/offline`: the run loop, supply models, and the summary.
- `state.Mem`: an in-memory vehicle pool with the same interface as Redis (`dispatch.Pool`).
- `cmd/offline`: one command → `summary.json` (params + stats), `trips.csv`, `samples.csv`.
- World changes: a min-heap of leg end times; `PingInterval = 0` turns pings off.

## Concepts

### Discrete-time simulation
The clock jumps in 1-second steps. Each step: adjust supply (once a minute), add requests that arrived, advance the world (pickups, dropoffs, expiries), run a dispatch round. No wall clock anywhere, so results don't depend on machine speed.

### Determinism, and how we got it
Three sources of nondeterminism had to be removed:
1. **Go map iteration order is random.** Every place that reads a map either sorts first (`IdleIDs`) or sorts the results (`sortCandidates` breaks distance ties by ID).
2. **Concurrency.** One goroutine, no channels.
3. **Randomness.** One seeded PCG generator, consumed in a fixed order.

Proof: two runs → identical SHA-1 for `trips.csv` and `samples.csv`.

### Interfaces make this cheap
`Greedy.Dispatch` takes a `Pool` interface. Redis and `Mem` both implement it, so the exact same policy code runs live and offline. Adding Hungarian or a CUDA matcher in Phase 2 means implementing `Policy` once; both modes get it.

### Data structures that mattered (each was found with the profiler, not guessed)
| Problem | Fix | Effect |
| --- | --- | --- |
| Scanning 22k vehicles every second for finished legs | min-heap keyed by leg end time | O(log n) per event |
| Nearest-car search scanning 169 grid cells | ring-by-ring search that stops early once the k-th best is closer than the next ring could be | test suite 4.52 s → 0.61 s |
| All cars spawned on one point at 00:00 | spread fallback + regression test | removed a full-fleet scan per query |

The ring-search stop rule: the query point lies inside the center cell, so any vehicle in ring d+1 is at least d × cell width away. If the k-th best so far is closer than that, no farther ring can change the answer. A test checks against brute force on 200 random queries.

### Supply that follows the data
The fleet logs on and off to follow `real busy vehicles at h:30 / utilization`, interpolated per minute. Idle drivers log off immediately; if there aren't enough, busy drivers log off after their current trip (`logoffDebt`).

### Warm-up and the edge of the data
A one-day file has no "hour before midnight", so full-day runs measure 01:00–24:00 after a 60-minute warm-up.

## First full-day result (utilization 0.75, greedy, 3-mile radius)
- Day level: simulated median wait **216 s** vs real **212 s**. Close, but misleading on its own:
- **Rush hours collapse.** 18:00: simulated median 1,482 s vs real 207 s, 3,095 riders unserved, simulated utilization 0.82–0.96.
- **Quiet hours are oversupplied**: simulated waits below real.

The rush-hour collapse is the **wild goose chase** (Castillo, Knoepfle & Weyl, "Surge Pricing Solves the Wild Goose Chase", 2017). When cars are scarce, greedy dispatches distant ones. Long pickup drives keep cars busy longer, which makes cars scarcer. With a 3-mile radius at 8.6 mph a pickup can take about 28 minutes. Real platforms prevent this with pickup-ETA caps and surge pricing; our replay has fixed demand and no cap.

Lesson: **a good day-level average can hide a broken regime.** Always look at the breakdown.

## Interview questions this answers
- "How do you make a simulation reproducible?" → seeds, no map-order dependence, single-threaded, checksums.
- "Why does your greedy dispatcher get worse when it's busy?" → wild goose chase; capped ETAs and batching both help, which is E2/E3.
- "How did you find the performance problems?" → `pprof` CPU profiles; each fix has a before/after number.
