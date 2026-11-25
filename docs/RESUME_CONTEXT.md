# Fleetline: resume context (as of 2026-11-25)

Source of truth for resume bullets. Every number links back to a script and result file in this repo.
**Rule: only items under "Built" may go on a resume.** Move items up from "Planned" only after they run and have a result file.

## Project line
**Fleetline: Real-Time Ride Dispatch Platform** · Go, Kafka (Redpanda), Redis/Lua, Python, LightGBM, PyTorch, ONNX Runtime, SciPy, OR-Tools, Next.js, deck.gl, Docker, GitHub Actions, Vercel
- Live demo: https://fleetline-sigma.vercel.app
- GitHub: github.com/Nijjea1/fleetline (**still private: make public before sending a resume**)

## Built: bullets (pick 3–5 per role)

**Systems / backend**
- Built a real-time dispatch platform in Go on Kafka and Redis that replays a full day of NYC Uber/Lyft demand (620k requests, 29k vehicles). Profiled the ingest path and raised peak throughput **22% to 187k GPS pings/s** on one laptop, while cutting p99 latency at 150k/s from **242 to 46 ms**. (bench/e1)
- Proved zero data loss under failure: with Redis frozen or killed, the backlog buffers in Kafka (up to 511k pings) and drains completely. The test also exposed a silent data-drop bug, which I fixed. (bench/e1, docs/learning/07)

**Optimization / algorithms**
- Cut average rider wait **18.6%** (median −18.4%, p90 −15.7%) vs greedy dispatch with a batched min-cost assignment solver, measured on a held-out day of **603k real trips** (3 seeds). Found and fixed rider starvation with an age-weighted objective. (bench/e2)
- Wrote an exact sparse assignment solver in Go that exploits city geography (near-linear scaling): **168 ms for 100k riders, 28× faster than Google OR-Tools and 217× faster than SciPy**, all verified to the same optimum. (bench/e4)

**ML**
- Trained a LightGBM arrival-time model that cuts error **54% vs OSRM routing** (MAE 515.7 → 236.5 s on 4.1M trips). Also benchmarked a PyTorch residual MLP (DeepETA-style, 266.1 s) and kept the better model. (ml/)
- Exported the PyTorch model to ONNX with identical test accuracy. Measured inference: ONNX Runtime CPU **0.009 ms** at batch 1, while the Apple GPU only wins above ~1k rows (2× at batch 8,192). (ml/, E6)

**Frontend / data viz**
- Built a deck.gl + MapLibre dashboard (Next.js, deployed on Vercel) that animates 29k vehicles over a real NYC street map from a 5.8 MB replay file, plus a live mode fed by the running system. (web/)

## Skills that are honestly earned by this project
Go, Kafka/Redpanda, Redis (Lua scripting), Docker Compose, Python, NumPy/SciPy, LightGBM, PyTorch, ONNX Runtime, OR-Tools, min-cost flow / assignment algorithms, load testing and profiling (Go pprof endpoints, Redis command stats), Next.js/React/TypeScript, deck.gl, MapLibre, GitHub Actions CI, Vercel.

## Planned, NOT built: do not claim yet
| Item | Status | What unlocks the claim |
| --- | --- | --- |
| CUDA assignment solver | Not started; GPU smoke test written, not run | Run on RTX 5060, beat Go's 25 ms at 30k riders (or report honestly that it doesn't) |
| NVIDIA cuOpt | Not started | Benchmark vs Go solver in bench/e4 |
| TensorRT inference | Script written (ml/tensorrt), never run | Run on RTX 5060, produce a latency result file |
| AWS / Terraform deploy | Not started | Infra in repo, deployed once, cost noted |
| Waymo motion model (E8) | Not started. Robotaxis exist only as a simulated vehicle type (geofenced, 10% of new cars), with no Waymo data | Train on Waymo Open Motion + result |
| Driver accept/decline model | Next on the plan | Model + re-run of E2 |

## Wording guardrails
- Don't say "faster than Uber" or compare waits with real Uber waits. The simulator has no driver accept/decline, so absolute waits are optimistic. Policy-vs-policy comparisons (the 18.6%) are fair.
- The PyTorch MLP **lost** to LightGBM. Say "benchmarked" or "compared", never "deployed a deep learning ETA model".
- The "Apple GPU" result is the M5 MacBook's GPU, not CUDA.
- If asked about the timeline: development started 2026-11-20 (the git history shows this).
