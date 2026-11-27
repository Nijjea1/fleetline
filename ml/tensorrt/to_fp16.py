"""Make an FP16 copy of the ETA ONNX model for TensorRT 11.

TensorRT 11 builds "strongly typed" networks: each layer runs in the precision the ONNX
graph declares, and the old `--fp16` flag is gone. So the FP16 engine is built from an
FP16 graph: weights and math in float16, inputs and outputs kept float32 (keep_io_types),
so callers feed and read the same tensors as the FP32 model.

    uv run --no-sync --with onnx --with onnxconverter-common python ml/tensorrt/to_fp16.py
"""

import sys
from pathlib import Path

import onnx
from onnxconverter_common import float16

src = Path(sys.argv[1] if len(sys.argv) > 1 else "data/models/eta_residual_mlp.onnx")
dst = src.with_name(src.stem + "_fp16.onnx")
model = float16.convert_float_to_float16(onnx.load(src), keep_io_types=True)
onnx.save(model, dst)
print(f"{src} -> {dst} ({dst.stat().st_size / 1e6:.2f} MB)")
