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
