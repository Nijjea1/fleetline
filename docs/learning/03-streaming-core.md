# 03 · The streaming core: worldsim, fleetd, Kafka, Redis

## What we built

```
                 ride.requests (key: H3 res-7 pickup cell)
 ┌──────────┐ ─────────────────────────────────────────────▶ ┌──────────┐      ┌───────┐
 │ worldsim │   vehicle.pings (key: H3 res-7 vehicle cell)   │  fleetd  │ ───▶ │ Redis │
 │ "the     │ ─────────────────────────────────────────────▶ │ "the     │ ◀─── │ state │
 │  city"   │   trip.events (pickup / dropoff / expired)     │  system" │      └───────┘
 │          │ ─────────────────────────────────────────────▶ │          │
 │          │ ◀───────────────────────────────────────────── │          │ ──▶ /metrics, /stats
 └──────────┘   dispatch.assignments (key: vehicle ID)       └──────────┘
```

| Package | Job |
| --- | --- |
| `internal/model` | The message contract: `Ping`, `RideRequest`, `Assignment`, `TripEvent` |
| `internal/geo` | Haversine, the calibrated travel model, H3 cells, the robotaxi zone polygon |
| `internal/bus` | Kafka producer/consumer on franz-go |
| `internal/state` | Redis vehicle store with atomic Lua scripts and a GEO index of idle cars |
| `internal/world` | The deterministic city simulator (no I/O, fully unit-tested) |
| `internal/dispatch` | `Policy` interface and the greedy policy |
| `internal/metrics` | Prometheus metrics + exact percentiles for benchmarks |
| `cmd/worldsim`, `cmd/fleetd` | The two programs |

## Concepts

### Separating "the world" from "the system" (D-006)
A simulator that also dispatches can cheat without meaning to: it knows exactly where every car is *right now*. A real dispatcher only knows what the last ping said, a few seconds late, possibly out of order. By forcing everything through Kafka, fleetd faces the same problems a production dispatcher does, and the bugs we fixed (stale pings, double assignment) are real distributed-systems bugs.

### Event time and watermarks (D-007)
Each message carries the simulated time it happened. fleetd's clock is the *watermark*: the newest event time it has seen. Dispatch rounds fire every `window` of event time. So a replay at 10× and at 30× make the same decisions, as long as fleetd keeps up (which, as we found, is not automatic).

### Why the Redis writes are Lua scripts (D-008)
Two races, both tested in `redis_integration_test.go`:
1. **Out-of-order pings.** Keying by H3 cell means a car crossing a cell boundary switches partitions, and Kafka only orders *within* a partition. Fix: drop any ping older than the stored one (`ts <= stored ts`). This also makes redelivered pings harmless (*idempotency*), which matters because Kafka consumers are *at-least-once*.
2. **Ping-before-ack.** The dispatcher claims car A at t=100. The car's ping from t=101 was sent before it received the assignment and still says "idle". Without a guard, A goes back in the idle pool and gets assigned twice. Fix: every assignment bumps a per-vehicle `pending_seq`; pings carry the last `seq` the car has seen; a ping with `seq < pending_seq` may update position but not status.

A Lua script runs atomically inside Redis, so "read, compare, write" can't interleave with another client. That is the same guarantee as a database transaction, at microsecond cost.

### Batching: the main throughput lever
- **Producer linger** (`ProducerLinger`): wait a few ms to pack many records into one request.
- **Consumer batch → Redis pipeline**: one Kafka poll (often thousands of pings) becomes one Redis round trip. In the first run, batches averaged ~2,400 pings.
- **Backpressure**: the producer's buffer is capped (`MaxBufferedRecords`); when Kafka is slow, `Produce` blocks instead of growing memory forever.

### Calibrating the simulator from data
Three numbers come from real trips instead of guesses (`fleetline-sim calibrate`):
- Detour factor 1.333 (road miles / straight-line miles).
- Speed by hour: 8.6 mph at 4–5 pm, 17.2 mph at 4 am.
- Busy vehicles by hour: 1,908 at 3:30 am up to 20,564 at 5:30 pm. Fleet size = busy / utilization (default 0.75, an explicit assumption).

### The first bug the metrics caught
The first run used a guessed fleet of 8,000 cars. Result: 21.5% of riders gave up. The data said 16,441 real cars were busy at 7:30 am, so supply was about half of reality. Lesson: **calibrate inputs before trusting outputs.**

The same run showed dispatch rounds averaging 146 ms while the 30× replay needed one every ~33 ms of wall time, so fleetd fell behind. Cause: greedy made two to three Redis round trips *per waiting request per round*. See the before/after numbers in `PROGRESS.md`.

## Interview questions this answers
- "Kafka only orders within a partition, and you key by location. How do you handle out-of-order updates?"
- "How do you avoid assigning the same driver twice?"
- "Is your consumer exactly-once?" (No: at-least-once plus idempotent writes, which is the usual production answer.)
- "How did you choose the fleet size?" (From the data, with utilization as a stated assumption.)
- "Tell me about a performance bottleneck you found." (Per-request Redis round trips in the dispatch round.)
