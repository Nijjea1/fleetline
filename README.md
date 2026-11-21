# Fleetline

Real-time dispatch for a mixed fleet of human drivers and robotaxis — GPU-accelerated matching, ML-predicted arrival times, and a benchmark behind every number.

> **Status:** Phase 1 (core) in progress. Numbers appear here only after a benchmark in `bench/` produces them.

## What it does

Fleetline replays a real day of NYC ride-hail demand, assigns every request to a vehicle inside a fixed time budget, and measures whether each change makes the city better served.

## Repo layout

| Path | What lives there |
| --- | --- |
| `sim/` | Data download, event replay, scenario configs (Python) |
| `core/` | Go ingest, vehicle state, dispatch service |
| `matcher/` | CPU baselines, CUDA kernel, pybind11 bindings |
| `eta/` | ETA model training, ONNX/TensorRT export, C++ server |
| `av/` | Robotaxi motion model (stretch) |
| `bench/` | Every experiment as a script, plus summarized results |
| `infra/` | Docker Compose, Terraform, Kubernetes, CI |
| `web/` | Next.js + deck.gl dashboard |
| `docs/` | Scope, decisions, datasets, progress log, learning notes |

## Docs

- [Project scope](docs/PROJECT_SCOPE.md)
- [Progress log](docs/PROGRESS.md)
- [Decision log](docs/DECISIONS.md)
- [Datasets](docs/DATASETS.md)
- [Learning notes](docs/learning/)

## Quick start (so far)

```bash
cd sim
uv run fleetline-sim download            # ~510 MB: Sep 2026 Uber/Lyft trips + zone shapes
uv run fleetline-sim prepare             # builds data/derived/day_2026-09-16_seed7.parquet
uv run pytest
make up                                  # from repo root: Redpanda, Console (localhost:8080), Redis
```
