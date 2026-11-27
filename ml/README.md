# ml/: Fleetline's ML/AI work

Everything machine-learning lives here: data prep, models, training, export, inference benchmarks and their results. Numbers in this file come from `results/*.json`, which the commands below write.

| Model | Question | Status | Experiment |
| --- | --- | --- | --- |
| **ETA (core)** | How long will a drive between two places take, at this time? | Baselines + residual MLP trained | E5, E6 |
| **Robotaxi motion (stretch)** | Where will nearby cars and pedestrians go in the next 8 s? | Not started: needs Waymo dataset access | E8 |

## Layout

```
ml/
  osrm/                  build.sh / serve.sh: OpenStreetMap road graph (NY + NJ) served by OSRM
  src/fleetline_ml/
    eta/
      osrm_matrix.py     263x263 zone-to-zone free-flow times (one OSRM /table call)
      weather.py         hourly Central Park weather (Open-Meteo archive)
      dataset.py         train/val/test from real trips, split by date
      baselines.py       B0-B3: OSRM, x global factor, x hour-of-week factor, LightGBM
      model.py           residual MLP (DeepETA-style)
      train.py           training loop (MPS on the Mac, CUDA on the RTX 5060)
      export.py          ONNX export, parity check, latency benchmark
    av/                  robotaxi motion model (stretch)
  tensorrt/
    to_fp16.py           FP16 copy of the ONNX graph (TensorRT 11 is strongly typed)
    bench.sh, parse.py   trtexec: FP32/TF32/FP16 engines, GPU compute time
    check.py             full-test-set accuracy per engine + latency from Python
  results/               every reported number, as JSON
  tests/
```

## Reproduce

```bash
ml/osrm/build.sh && ml/osrm/serve.sh          # road graph + OSRM on :5001 (Docker)
(cd sim && uv run fleetline-sim zone-points)   # one routable point per taxi zone
cd ml
uv run fleetline-ml osrm-matrix                # zone-to-zone free-flow times
uv run fleetline-ml weather                    # Sep 2026 hourly weather
uv run fleetline-ml dataset                    # 13.8M / 1.9M / 4.1M rows
uv run fleetline-ml baselines                  # E5 part 1 -> results/e5_baselines.json
uv run fleetline-ml train                      # E5 part 2 -> results/e5_residual_mlp.json
uv run fleetline-ml export                     # E6 (Mac) -> results/e6_inference_mac.json
# E6 on the RTX 5060 (WSL2, TensorRT 11.5), from the repo root:
bash ml/tensorrt/bench.sh 2>&1 | tee ml/results/e6_tensorrt_raw.txt && python3 ml/tensorrt/parse.py
ml/.venv/bin/python ml/tensorrt/check.py      # -> results/e6_tensorrt_python.json
```

## ETA model

**Task.** Predict real pickup→dropoff duration for an Uber/Lyft trip, using only what's known at request time.

**Data.** NYC High-Volume FHV trips, September 2026. Split **by date**: train Sep 1–21, validation Sep 22–24, test Sep 25–30. A random split would leak, because trips in the same hour share traffic.

**Resolution.** The public data gives taxi zones, not coordinates, so the routing baseline is zone-to-zone: OSRM time between each zone's representative point. Some error is irreducible at this resolution (where inside the zone the rider really was).

**No leakage.** `trip_miles` is not a feature (it's the route actually driven, known only afterwards). Shared rides are excluded. The implausible-speed filter is applied to training data only.

**Models, simplest first:**
| | Model | Idea |
| --- | --- | --- |
| B0 | OSRM | Free-flow road time; knows roads, not traffic |
| B1 | OSRM × global factor | One traffic multiplier |
| B2 | OSRM × hour-of-week factor | 168 multipliers: rush hour ≠ Sunday morning |
| B3 | LightGBM (L1) | Gradient-boosted trees on all features |
| M1 | Residual MLP | `B2 + network(zone, zone-pair, hour-of-week, OSRM, time, weather)`; L1 loss |

**Test-set results (4.1M trips, Sep 25–30):** see `results/e5_*.json`.

| Model | MAE (s) | p90 abs error (s) | Bias (s) |
| --- | --- | --- | --- |
| B0 OSRM | 515.7 | 1,106.5 | −493.1 |
| B1 × global | 348.4 | 797.1 | +1.5 |
| B2 × hour-of-week | 314.4 | 704.6 | +0.5 |
| B3 LightGBM | 236.5 | 503.2 | −14.7 |
| M1 residual MLP (h256, 4 epochs, 63 s on M5 GPU) | 266.1 | 572.3 | — |
| M1 wide (h512, 12 epochs, 442 s) | 268.1 | 584.0 | +35.4 |

**Verdict so far: LightGBM wins.** The residual MLP beats every non-ML baseline by ~15%, but a wider network trained 3× longer is no better (validation plateaus at ~274 s by epoch 5), so capacity and training time aren't the limit. Next ideas, each a measured experiment: feed LightGBM's prediction as the base (stacking), add zone-pair historical medians as a feature, try a pickup-time-of-day × zone-pair interaction.

**Why residual?** Uber's DeepETA predicts a correction to a routing engine's estimate rather than the whole time. The routing engine already knows the map; the network only has to learn traffic and local quirks. With a zero-initialized output layer, the untrained network predicts exactly the baseline, so training can only improve on it.

## Inference (E6)

Dispatch scores many (car, rider) pairs per round, so latency **per batch** matters. ONNX export matches PyTorch to within 0.0005 s and gives the identical test MAE (266.10 s).

| Batch | PyTorch CPU | ONNX Runtime CPU | PyTorch MPS (M5 GPU) |
| --- | --- | --- | --- |
| 1 | 0.032 ms | **0.009 ms** | 0.207 ms |
| 256 | 0.141 ms | **0.102 ms** | 0.309 ms |
| 1,024 | 0.315 ms | 0.314 ms | 0.386 ms |
| 8,192 | 2.371 ms | 2.582 ms | **1.220 ms** |

p50 latency, 200 reps after warm-up, idle M5 MacBook. CPU wins up to ~1k rows; the M5 GPU is ~2× faster at 8k. (An earlier run taken while training was using the CPU showed 20 ms at 8k: a reminder to benchmark on an idle machine.) Every runtime serves the same ONNX file. Mac numbers: `results/e6_inference_mac.json`. TensorRT numbers (RTX 5060) are below.

### TensorRT on the RTX 5060

Same ONNX file, three engines: **FP32** (TF32 off), **TF32** (TensorRT's default: FP32 storage, tensor-core matmuls) and **FP16** (an FP16 copy of the graph, since TensorRT 11 is strongly typed and dropped the `--fp16` flag).

**Accuracy, all 4,086,192 test trips** (`results/e6_tensorrt_python.json`):

| Engine | Test MAE | vs PyTorch FP32 | Largest per-trip difference |
| --- | --- | --- | --- |
| PyTorch FP32 (CUDA) | 266.096 s | | |
| TensorRT FP32 | 266.096 s | +0.000 s | 0.0015 s |
| TensorRT TF32 | 266.099 s | +0.003 s | 2.2 s |
| TensorRT FP16 | 266.098 s | +0.003 s | 5.5 s |

FP16 changes individual predictions by up to 5.5 s (0.3 s on average) but the error over the test set by only 0.003 s, well inside the 0.5 s bar set for this check.

**Latency from Python**, timed like the Mac table (inputs already on the GPU, synchronise after each call, 200 reps): p50, median of 3 runs (range in brackets):

| Batch | PyTorch CUDA | TensorRT FP32 | TensorRT FP16 |
| --- | --- | --- | --- |
| 1 | 1.163 ms | 0.164 ms (0.161–0.303) | **0.131 ms** (0.119–0.142) |
| 256 | 0.865 ms | 0.274 ms (0.224–0.393) | **0.145 ms** (0.123–0.155) |
| 1,024 | 0.833 ms | 0.179 ms (0.168–0.291) | 0.215 ms (0.127–0.274) |
| 8,192 | 0.845 ms | 0.524 ms (0.519–0.527) | **0.375 ms** (0.285–0.457) |

**GPU compute only** (`trtexec`, ≥ 5 s per engine, `results/e6_tensorrt.json`): 0.041 ms (FP32) vs 0.031 ms (FP16) at batch 1; at 8,192, 0.287 ms (FP32), 0.189 ms (TF32), **0.095 ms (FP16, 3.0× FP32)**.

What this shows:
- **TensorRT FP16 is 2.3–8.9× faster than eager PyTorch on the same GPU** (batch 1: 1.16 → 0.13 ms; batch 8,192: 0.85 → 0.38 ms); FP32 engines 1.6–7.1×. PyTorch launches one kernel per layer from Python; TensorRT fuses layers into a few kernels.
- **Below ~1k rows, overhead decides, not precision.** The GPU computes a batch of 1 in 0.03–0.04 ms, but a call from Python costs ~0.13 ms (launch + synchronise, under WSL2). FP32 and FP16 overlap within their run-to-run ranges up to 1,024 rows.
- **FP16 pays off on big batches**: 3.0× FP32 in pure GPU time at 8,192 rows, 1.4× as a Python caller sees it.
- Across machines (different hardware, so indicative only): at batch 1 the Mac's ONNX Runtime CPU (0.009 ms) is still ~15× faster than any GPU call here; at 8,192 TensorRT FP16 (0.375 ms) beats the M5 GPU (1.22 ms) and ONNX Runtime CPU (2.58 ms). For dispatch-sized batches (thousands of car × rider pairs per round) the GPU path wins; for one-off ETA queries the CPU does.
- **Not done: INT8.** It needs a calibration pass on real rows to be meaningful; the FP16 result already leaves little latency to win at these sizes.

## Robotaxi motion model (stretch, E8)

Needs the Waymo Open Motion Dataset (non-commercial license; accept the terms at waymo.com/open, then download with `gsutil`). Plan: constant-velocity baseline → small vectorized transformer predicting several future paths, sized for 8 GB VRAM; use predictions to flag robotaxi pickups that conflict with predicted traffic. Not started.
