# Progress Log

A running record of what was built, when, and what is next. Dates are real work dates.

## Phase 1 · Core

| Step | Status |
| --- | --- |
| 0. CUDA test kernel + PyTorch on RTX 5060 | ⬜ To do on the GPU machine |
| 1. Repo skeleton, docs, docker compose | ✅ 2026-11-20 |
| 2. Download data, build replay day | ✅ 2026-11-20 |
| 3. Event replayer → Kafka | ⬜ Needs Docker |
| 4. Go ingest, H3-keyed Kafka, Redis state | ⬜ |
| 5. Greedy nearest-driver dispatch + metrics | ⬜ |
| 6. deck.gl dashboard (live + replay), Vercel | ⬜ |
| 7. E1 load test, README + architecture diagram | ⬜ |

## 2026-11-20
- Installed Go 1.27.4, gh 2.105.0, uv 0.13.9. Docker Desktop needs an admin password, so the user installs it.
- Researched datasets; chose NYC High-Volume FHV (Uber/Lyft) for its request and on-scene timestamps (D-001). Verified that TLC's republished 2015 files no longer contain lat/lon.
- Monorepo scaffold, decision log, dataset doc, docker compose (Redpanda, Console, Redis), Makefile.
- `sim/`: downloader, seeded zone sampler (4 tests), replay-day builder. Replay day 2026-09-16 → 635,294 trips, deterministic.
- Learning notes 01 and 02.

### Next
- Install Docker, `make up`, then the replayer that publishes requests and simulated driver pings to Redpanda.
- Go ingest service.
