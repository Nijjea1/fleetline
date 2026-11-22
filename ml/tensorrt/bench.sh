#!/usr/bin/env bash
# E6 on the RTX 5060: build TensorRT engines from the same ONNX file and time them.
#
# STATUS: written on the Mac, NOT YET RUN. Run on the GPU machine after copying
# data/models/eta_residual.onnx there. Results go in ml/results/e6_tensorrt.json by hand
# (or extend this script) together with the GPU/driver/TensorRT versions.
#
# FP32 and FP16 are measured here with trtexec. INT8 needs a calibration set (real
# inputs, so TensorRT can pick per-tensor scales) to say anything about accuracy; that
# is a Python calibrator using ~10k rows of val.parquet, to be added on the GPU box.
set -euo pipefail
ONNX="${1:-data/models/eta_residual.onnx}"
OUT="${OUT:-data/models}"

for B in 1 32 256 1024 8192; do
  SHAPES="zones:${B}x2,how:${B},cont:${B}x10,base:${B}"
  for PREC in fp32 fp16; do
    FLAG=""; [ "$PREC" = fp16 ] && FLAG="--fp16"
    echo "== batch $B $PREC"
    trtexec --onnx="$ONNX" $FLAG \
      --minShapes="zones:1x2,how:1,cont:1x10,base:1" \
      --optShapes="$SHAPES" --maxShapes="zones:8192x2,how:8192,cont:8192x10,base:8192" \
      --shapes="$SHAPES" --saveEngine="$OUT/eta_${PREC}_b${B}.plan" \
      --warmUp=500 --iterations=2000 2>&1 | grep -E "GPU Compute Time|Throughput|Latency: min"
  done
done
