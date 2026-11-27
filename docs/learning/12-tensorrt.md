# 12 · Serving a model with TensorRT (E6 on the RTX 5060)

## What TensorRT is
PyTorch runs a model **eagerly**: Python calls one GPU kernel per layer (embedding, matmul, ReLU, …), and each call has a fixed cost to launch. TensorRT is NVIDIA's inference **compiler**. It reads the model once (here from the same ONNX file the Mac uses), then **fuses** layers (matmul + bias + ReLU in one kernel), tries several kernel implementations for your exact GPU and batch sizes, and saves the fastest plan as an **engine**. At run time there's no Python per layer, just "run this engine".

## Precision: FP32, TF32, FP16
- **FP32**: 32-bit floats, the training precision. 
- **TF32**: still stored as FP32, but matrix multiplies run on tensor cores with a shorter (10-bit) mantissa. Usually free speed; TensorRT turns it on by default (`--noTF32` turns it off).
- **FP16**: 16-bit floats, half the memory traffic and much faster tensor-core math, but only ~3 significant digits. Risk: predictions change.

TensorRT 11 builds **strongly typed** networks: each layer runs in the type the ONNX graph says, and the old `--fp16` "let TensorRT pick" flag is gone. So we make an FP16 copy of the graph (`ml/tensorrt/to_fp16.py`, inputs and outputs kept FP32) and build from that.

## Always check accuracy, not just speed
`ml/tensorrt/check.py` runs every engine over all 4.1M test trips. FP16 moved single predictions by up to 5.5 s, but the test MAE by only 0.003 s (266.096 → 266.098 s). Rounding errors go both ways and cancel out on average; a 266 s error dwarfs them.

## Two ways to time, two different answers
- `trtexec` reports **GPU compute time**: batch 1 takes 0.03–0.04 ms.
- From Python (what a caller sees), the same call takes ~0.13 ms: launching work and waiting for the GPU (`synchronize`) cost more than the math, especially under WSL2.

So for one-row queries the CPU wins (the Mac's ONNX Runtime does it in 0.009 ms); for thousands of rows at once (a dispatch round scoring every car × rider pair) the GPU wins, and FP16 matters (3× FP32 in GPU time at 8,192 rows).

## Measuring honestly
- Repeat the whole timing pass (3×) and report the range: at small batches FP32 and FP16 swapped places between runs, so we don't claim an order there.
- The Mac and the RTX laptop are different machines: cross-machine comparisons are labelled "indicative". The same-machine baseline is PyTorch on CUDA.
- INT8 was skipped: it needs calibration on real data to say anything about accuracy.

## Interview questions this answers
- "Why is TensorRT faster than PyTorch?" (Layer fusion, kernel auto-tuning, no per-layer Python.)
- "Did FP16 hurt accuracy? How do you know?" (Full test set: +0.003 s MAE.)
- "When would you *not* use a GPU for inference?" (Single-row, latency-critical calls where launch overhead dominates.)
