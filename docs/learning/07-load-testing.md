# 07 · Load testing the ingest path (E1)

## The question
How many vehicle pings per second can fleetd take (Kafka → decode → Redis) on one laptop, what limits it, and what happens when a dependency stalls?

## Method
- **Open loop.** `cmd/loadgen` sends pings on a fixed schedule no matter how fleetd is doing. A closed loop (send the next ping when the last is acknowledged) slows down with the system and hides overload. Real vehicles don't wait for the server.
- **Measure at the system, not the sender.** Every step scrapes fleetd's Prometheus counters before and after: *applied* = pings that reached Redis; *latency* = broker append → Redis write, from histogram bucket deltas (the same interpolation as PromQL's `histogram_quantile`).
- **"Sustained" is strict.** ≥ 99% of what was offered got applied, and the backlog at the end of a 30 s step is < 0.1 s of input. "Applied 135k of 150k" is not sustaining 150k.
- **No dispatch noise.** Only pings are sent, so the dispatch loop does no work.
- **Spread.** 3 runs per rate; tables show the median and the min–max.

## Results (on AC power, Docker Desktop)
| Version | Max sustained | p50 / p99 there |
| --- | --- | --- |
| Baseline: 1 Lua call per ping, batches applied serially | 130k pings/s | 47 / 112 ms |
| + 256 pings per Lua call | 130k pings/s | 28 / 64 ms |
| + 4 concurrent apply workers | **150k (3/3), 175k (2/3)** | 25 / 112 ms at 175k |

Final interleaved A/B (3 pairs): peak applied **153k → 187.6k pings/s (+22%)**; at 150k, p99 **242 → 46 ms**. Inline's result varied run to run (150k passed 2/3); the workers version was steadier, and that's a finding too.

### Fix 1 helped latency, not throughput, and that's the lesson
The first profile looked like "Redis spends its time on script-call overhead", so we batched 256 pings into each call. Latency halved, but the ceiling barely moved. Re-profiling (with Redis's counters **reset first**) showed why:
- Redis: 5 µs per ping (2.6 µs of real commands, ~2.4 µs of Lua loop), but only **82% busy**.
- fleetd: 25% busy, mostly waiting in network syscalls.

fleetd's loop was strictly serial: decode a batch, send it, *wait*, decode the next. Redis sat idle while fleetd decoded. And the first diagnosis had used Redis's *lifetime* counters, which mixed in older, different traffic. **Reset counters before you measure.**

### Fix 2: overlap decode with Redis writes
Four apply workers fed by a bounded queue. Pings for the same vehicle can now be applied out of order across batches. That's safe only because of the timestamp guard from D-008 (older pings are dropped as stale). Design decisions compound: a guard added for correctness made a performance change safe. The trade-off (a crash can lose up to 4 queued batches of pings, which heal within one 4 s ping interval) is written down in D-014.

## Backpressure (C4)
Steady 50k pings/s, then:

| Fault | Peak backlog | Recovery | Lost |
| --- | --- | --- | --- |
| Redis frozen 10 s (`docker pause`) | 511k pings | drained in ~8 s at ~124k/s | **0** (3,049,472 sent = applied) |
| Redis down 5 s (`docker stop`) | 244k pings | 5 retries with backoff | **0** (3,049,463 = 3,049,463) |

How: when Redis is slow, apply workers block, the bounded queue fills, the consumer stops polling, and the backlog waits **in Kafka**, which is built to hold it. Memory in fleetd stays bounded. Before this test, a Redis *error* dropped the batch, and the counter labelled it `error` but the load generator counted it as processed, so the loss was invisible. **A fault-injection test found a data-loss bug that throughput tests never would.**

## Four ways the measurements lied, and the fixes
1. **Counting rejected sends as sent.** At each step boundary a few produces hit a cancelled context and were rejected, but were counted. Drain never "finished". Fix: count only broker-acknowledged records.
2. **Inheriting a backlog.** fleetd's consumer group resumes from committed offsets. After an interrupted run, the next fleetd read minutes-old pings and recorded 30 s+ latencies. Fix: delete the consumer group before each run.
3. **Machine drift.** After ~25 minutes, every Redis op cost 2× (GEOADD 1.9 → 3.6 µs) with identical data. A rate sustained earlier failed. Fix: interleave A/B runs (A, B, A, B…) with cool-downs, so drift hits both equally.
4. **The laptop was unplugged.** `pmset -g batt`: on battery, with Low Power Mode on. The power log showed the switch at 23:03, exactly when results degraded. Every reported run is from the AC window. **Record the environment (power, other load) with every benchmark.**

## Interview questions this answers
- "Open vs closed loop load testing?" / "How do you define 'sustained'?"
- "Your fix didn't improve throughput. What did you learn?"
- "What happens to your system when Redis goes down?"
- "Tell me about a benchmark result you didn't trust."
