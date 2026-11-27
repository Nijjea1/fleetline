#!/usr/bin/env bash
# E6 on the RTX 5060: build TensorRT engines from the same ONNX model and time them.
#
#   bash ml/tensorrt/bench.sh 2>&1 | tee ml/results/e6_tensorrt_raw.txt
#   python3 ml/tensorrt/parse.py          # -> ml/results/e6_tensorrt.json
#
# Written for TensorRT 11, which builds strongly typed networks (no --fp16 flag):
#   fp32  the ONNX model as exported, TF32 disabled (--noTF32): true FP32 math
#   tf32  the same model with TensorRT's default (FP32 storage, TF32 tensor-core matmuls)
#   fp16  an FP16 copy of the graph from to_fp16.py (inputs/outputs stay FP32)
# trtexec reports pure GPU compute time: in TensorRT 11, CUDA graphs and spin-wait are on
# and host<->device copies are excluded by default. INT8 needs a calibrator on real rows
# (val.parquet) to say anything about accuracy; not done here.
set -euo pipefail
ONNX="${1:-data/models/eta_residual_mlp.onnx}"
FP16="${ONNX%.onnx}_fp16.onnx"
OUT="${OUT:-data/models}"

[ -f "$FP16" ] || uv run --no-project --with onnx --with onnxconverter-common python ml/tensorrt/to_fp16.py "$ONNX"
echo "== env"
nvidia-smi --query-gpu=name,driver_version,memory.total,compute_cap --format=csv,noheader
echo "cuda $(nvcc --version | tail -1)"
trtexec --help 2>&1 | awk "NR==1"

for B in 1 32 256 1024 8192; do
  SHAPES="zones:${B}x2,how:${B},cont:${B}x10,base:${B}"
  for PREC in fp32 tf32 fp16; do
    MODEL="$ONNX"; FLAG=""
    [ "$PREC" = fp32 ] && FLAG="--noTF32"
    [ "$PREC" = fp16 ] && MODEL="$FP16"
    echo "== batch $B $PREC"
    trtexec --onnx="$MODEL" $FLAG \
      --minShapes="zones:1x2,how:1,cont:1x10,base:1" \
      --optShapes="$SHAPES" --maxShapes="zones:8192x2,how:8192,cont:8192x10,base:8192" \
      --shapes="$SHAPES" --saveEngine="$OUT/eta_${PREC}_b${B}.plan" \
      --warmUp=500 --iterations=2000 --duration=5 2>&1 | grep -E "GPU Compute Time|Throughput|Latency: min"
  done
done
