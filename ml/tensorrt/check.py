"""E6 on the RTX 5060, part 2: TensorRT accuracy on the full test set + latency from Python.

bench.sh/trtexec measures pure GPU compute time. This script answers the two questions it
can't: (1) does FP16 still predict as well (test MAE vs PyTorch FP32)? and (2) what does a
caller see from Python, timed the same way as the Mac numbers (export._time: 200 reps after
warm-up, inputs already on the device, synchronise after each call)? PyTorch on CUDA is
timed the same way as the same-machine baseline.

    ml/.venv/bin/python ml/tensorrt/check.py      # -> ml/results/e6_tensorrt_python.json

TensorRT's Python bindings come from apt (python3-libnvinfer, matching the installed
TensorRT) rather than a ~1 GB pip wheel that would duplicate the libraries.
"""

import json
import sys
from pathlib import Path

import numpy as np
import polars as pl
import torch

try:
    import tensorrt as trt
except ImportError:
    sys.path.append(f"/usr/lib/python3.{sys.version_info.minor}/dist-packages")
    import tensorrt as trt

from fleetline_ml.eta.export import INPUTS, ONNX_PATH, _time, load
from fleetline_ml.eta.train import predict, tensors
from fleetline_ml.paths import ETA_DIR, RESULTS_DIR

CHUNK = 65536  # rows per call for the full-test-set accuracy pass
BATCHES = (1, 32, 256, 1024, 8192)
REPEATS = 3
LOGGER = trt.Logger(trt.Logger.WARNING)


def build(onnx_path: Path, tf32: bool) -> trt.ICudaEngine:
    builder = trt.Builder(LOGGER)
    network = builder.create_network(0)  # TensorRT 11: explicit batch + strongly typed
    parser = trt.OnnxParser(network, LOGGER)
    if not parser.parse(onnx_path.read_bytes()):
        raise RuntimeError([str(parser.get_error(i)) for i in range(parser.num_errors)])
    config = builder.create_builder_config()
    if not tf32:
        config.clear_flag(trt.BuilderFlag.TF32)
    profile = builder.create_optimization_profile()
    for name, cols in {"zones": (2,), "how": (), "cont": (10,), "base": ()}.items():
        profile.set_shape(name, (1, *cols), (1024, *cols), (CHUNK, *cols))
    config.add_optimization_profile(profile)
    plan = builder.build_serialized_network(network, config)
    return trt.Runtime(LOGGER).deserialize_cuda_engine(plan)


class Runner:
    """Runs a TensorRT engine on torch CUDA tensors (torch owns the GPU memory)."""

    def __init__(self, engine: trt.ICudaEngine):
        self.engine, self.ctx = engine, engine.create_execution_context()
        self.out = torch.empty(CHUNK, dtype=torch.float32, device="cuda")
        self.stream = torch.cuda.current_stream().cuda_stream

    def __call__(self, t: dict[str, torch.Tensor]) -> torch.Tensor:
        n = len(t["base"])
        for k in INPUTS:
            self.ctx.set_input_shape(k, tuple(t[k].shape))
            self.ctx.set_tensor_address(k, t[k].data_ptr())
        self.ctx.set_tensor_address("eta_s", self.out.data_ptr())
        self.ctx.execute_async_v3(self.stream)
        torch.cuda.synchronize()  # GPU work is async: wait, or we time nothing
        return self.out[:n]


def main() -> None:
    model, prep = load()
    model = model.cuda()
    test = tensors(pl.read_parquet(ETA_DIR / "test.parquet"), prep, "cuda")
    test = {k: v.contiguous() for k, v in test.items()}
    y = test["y"].cpu().numpy()
    ref = predict(model, test, CHUNK)  # PyTorch FP32 on CUDA

    fp16_path = ONNX_PATH.with_name(ONNX_PATH.stem + "_fp16.onnx")
    engines = {
        "tensorrt_fp32": (ONNX_PATH, False),
        "tensorrt_tf32": (ONNX_PATH, True),
        "tensorrt_fp16": (fp16_path, True),
    }
    accuracy = {"pytorch_cuda_fp32": {"test_mae_s": float(np.abs(ref - y).mean())}}
    runners = {}
    for name, (path, tf32) in engines.items():
        run = runners[name] = Runner(build(path, tf32))
        pred = np.concatenate(
            [
                run({k: test[k][i : i + CHUNK] for k in INPUTS}).cpu().numpy()
                for i in range(0, len(y), CHUNK)
            ]
        )
        mae = float(np.abs(pred - y).mean())
        accuracy[name] = {
            "test_mae_s": mae,
            "mae_minus_pytorch_s": mae - accuracy["pytorch_cuda_fp32"]["test_mae_s"],
            "max_abs_diff_vs_pytorch_s": float(np.abs(pred - ref).max()),
            "mean_abs_diff_vs_pytorch_s": float(np.abs(pred - ref).mean()),
        }
        print(name, accuracy[name], flush=True)

    # Small batches are dominated by launch + sync overhead (noisy under WSL2), so the whole
    # latency pass runs REPEATS times; we report the median p50/p99 and the p50 spread.
    runs: dict[tuple[str, int], list[dict]] = {}
    with torch.no_grad():
        for _ in range(REPEATS):
            for b in BATCHES:
                sl = {k: test[k][:b].contiguous() for k in INPUTS}

                def run_torch(sl=sl):
                    out = model(*(sl[k] for k in INPUTS))
                    torch.cuda.synchronize()
                    return out

                fns = {"pytorch_cuda": run_torch}
                fns |= {n: (lambda r=r, sl=sl: r(sl)) for n, r in runners.items()}
                for name, fn in fns.items():
                    runs.setdefault((name, b), []).append(_time(fn, 200))
    rows = []
    for (name, b), rs in runs.items():
        p50s = [r["p50_ms"] for r in rs]
        p50 = float(np.median(p50s))
        rows.append(
            {
                "runtime": name,
                "batch": b,
                "p50_ms": p50,
                "p99_ms": float(np.median([r["p99_ms"] for r in rs])),
                "p50_min_ms": min(p50s),
                "p50_max_ms": max(p50s),
                "rows_per_s": b / (p50 / 1000),
            }
        )
        print(rows[-1], flush=True)

    out = {
        "machine": torch.cuda.get_device_name(),
        "torch": torch.__version__,
        "tensorrt": trt.__version__,
        "test_rows": len(y),
        "method": "Python, inputs already on the GPU, synchronise after each call; "
        "fleetline_ml.eta.export._time (200 reps after warm-up), same as e6_inference_mac.json; "
        f"whole pass repeated {REPEATS}x: median p50/p99, p50 min-max",
        "accuracy": accuracy,
        "latency": rows,
    }
    dest = RESULTS_DIR / "e6_tensorrt_python.json"
    dest.write_text(json.dumps(out, indent=2) + "\n")
    print(f"-> {dest}")


if __name__ == "__main__":
    main()
