# 05 · The ETA model: OSRM, baselines, a residual network, and ONNX

## What we built
- **OSRM** in Docker on a pinned OpenStreetMap snapshot (NY + NJ), clipped to NYC with `osmium`.
- A **263 × 263 zone-to-zone** free-flow time matrix (one `/table` request).
- **Hourly weather** from Open-Meteo's archive.
- A **dataset** of 19.8M real trips, split by date.
- **Baselines B0–B3** and a **residual MLP**, all scored on the same 4.1M held-out trips.
- **ONNX export** with a parity check and a latency benchmark; a TensorRT script for the RTX 5060.

## Concepts

### Routing engines: what OSRM knows and doesn't
OSRM turns OpenStreetMap into a graph (intersections = nodes, road segments = edges weighted by length and speed limit) and finds shortest paths with **Multi-Level Dijkstra**: the graph is pre-partitioned into cells, so a query explores only the cells it needs. It knows one-way streets and turn restrictions. It does **not** know traffic. Result: raw OSRM under-predicts NYC trips by **493 s on average** (B0 bias).

Two real bugs we hit:
- Geofabrik's "latest" file pointed at a snapshot not yet published, so we pin dated snapshots, which also makes the graph reproducible.
- Newark Airport is in New Jersey. Without NJ roads, OSRM snapped EWR to Staten Island (22.6 mi to Midtown instead of ~16.9 mi). Lesson: check routes you know the answer to.

### Splitting by time, not at random
Trips in the same hour share the same traffic. A random split puts some of the test hour's trips in training, so the model "remembers" that hour's congestion and the score is too good. Splitting by date (train Sep 1–21, val 22–24, test 25–30) mimics deployment: train on the past, predict the future.

### Leakage checklist
- `trip_miles` is the distance actually driven, known only after the trip. Not a feature.
- Filters that use the target (implausible speeds) apply to **train only**, so the test set keeps the hard cases.

### Baselines first
| Model | MAE (s) | What it adds |
| --- | --- | --- |
| B0 OSRM | 515.7 | roads |
| B1 × one factor | 348.4 | "traffic exists" |
| B2 × 168 hour-of-week factors | 314.4 | "traffic depends on the clock" |
| B3 LightGBM | 236.5 | non-linear interactions (zone × hour × weather) |

Each row is a hypothesis you can state in one sentence, and each improvement is measured. Without B0–B2 you couldn't say how much of the ML gain is "just multiply by 1.5".

### The residual network (DeepETA idea)
`prediction = B2 + network(features)`. The routing baseline already captures most of the structure, so the network learns a smaller, easier correction.
- **Embeddings** turn IDs (zone 161, hour-of-week 41) into learned vectors, so similar zones end up near each other.
- **Feature hashing** for zone pairs: 69,169 pairs share 2^17 rows of an embedding table. Collisions are rare and cheap; the alternative (one-hot pairs) is huge and sparse.
- **sin/cos time of day** so 23:59 and 00:00 are neighbors.
- **L1 loss** optimizes MAE directly. Squared error would chase rare 2-hour outliers.
- **Zero-initialized output layer**: the untrained model predicts exactly B2 (a unit test checks this), so training starts from a good place.

### Result so far, and why it's not a failure
First full run (hidden 256, 4 epochs, 63 s on the M5's GPU): **266.1 s**, better than B2 (314.4) but **worse than LightGBM (236.5)**.

Then the obvious test: is the network too small or under-trained? Hidden 512 for 12 epochs (7 min): **268.1 s**, no better. Validation MAE flattened near 274 s by epoch 5. So it isn't capacity or training time; the trees are getting information out of the features that the MLP isn't (GBDTs split sharply on exact zone IDs and thresholds, which is what zone-level data rewards).

Gradient-boosted trees are very strong on tabular data, and published benchmarks agree. "My neural net lost to LightGBM, here's how I ruled out capacity and what I'd try next" is a stronger interview answer than a suspicious win.

### ONNX, and when the GPU wins (E6)
ONNX is a portable graph format: PyTorch exports it, and ONNX Runtime and TensorRT run it. The exported model matches PyTorch to within 0.0005 s per prediction and gives the identical test MAE.

| Batch | ONNX Runtime CPU | PyTorch on M5 GPU (MPS) |
| --- | --- | --- |
| 1 | 0.009 ms | 0.207 ms |
| 1,024 | 0.314 ms | 0.386 ms |
| 8,192 | 2.582 ms | 1.220 ms |

At batch 1 the CPU wins by ~23×: launching GPU work costs more than the math. The crossover is around 1k rows; at 8k the GPU is ~2× faster. The model is small (2.2M parameters), so the GPU never gets much to chew on.

**Benchmarking lesson:** the first run happened while a training job was using the CPU, and showed CPU at ~24 ms for 8k rows, which looked like a 12× GPU win. Re-run on an idle machine: 2.6 ms, a 2× win. Always benchmark on a quiet machine and record what else was running. Dispatch scores all (car, rider) pairs in a round at once, so batching is what makes the GPU worth it. This is the same "the GPU only wins above N" story the CUDA matcher will tell in Phase 2.

## Interview questions this answers
- "How do you split time-series data?" / "What is leakage? Give an example from your project."
- "Why predict a residual?" / "Why L1?"
- "Why would a GBDT beat your neural net?"
- "When is GPU inference slower than CPU?"
