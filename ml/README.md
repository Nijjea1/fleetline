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
  tensorrt/bench.sh      TensorRT FP32/FP16 engines + timing (run on the RTX 5060)
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
| M1 residual MLP | *pending full run* | | |

**Why residual?** Uber's DeepETA predicts a correction to a routing engine's estimate rather than the whole time. The routing engine already knows the map; the network only has to learn traffic and local quirks. With a zero-initialized output layer, the untrained network predicts exactly the baseline, so training can only improve on it.

## Inference (E6)

Dispatch scores many (car, rider) pairs per round, so latency **per batch** matters. Every runtime serves the same ONNX file. Mac numbers: `results/e6_inference_mac.json`. TensorRT FP32/FP16 (and INT8 with calibration) run on the RTX 5060 via `tensorrt/bench.sh`.

## Robotaxi motion model (stretch, E8)

Needs the Waymo Open Motion Dataset (non-commercial license; accept the terms at waymo.com/open, then download with `gsutil`). Plan: constant-velocity baseline → small vectorized transformer predicting several future paths, sized for 8 GB VRAM; use predictions to flag robotaxi pickups that conflict with predicted traffic. Not started.
