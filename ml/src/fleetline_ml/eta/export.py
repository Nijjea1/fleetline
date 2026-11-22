"""E6: export the ETA model to ONNX and measure inference latency.

ONNX is the hand-off format: TensorRT (on the RTX 5060) builds its FP32/FP16/INT8
engines from this same file, so every runtime serves the identical model.

On the Mac we measure PyTorch (CPU and MPS) and ONNX Runtime (CPU) across batch sizes.
Dispatch calls the ETA model in batches (all candidate cars x all waiting riders in a
round), so latency per *batch* is what matters, and throughput per row is reported too.
"""

import json
import platform
import time

import numpy as np
import onnxruntime as ort
import polars as pl
import torch

from fleetline_ml.eta.model import ResidualETA
from fleetline_ml.eta.train import Prep, predict, tensors
from fleetline_ml.paths import ETA_DIR, MODELS_DIR, RESULTS_DIR

ONNX_PATH = MODELS_DIR / "eta_residual.onnx"
INPUTS = ["zones", "how", "cont", "base"]


def load() -> tuple[ResidualETA, Prep]:
    ckpt = torch.load(MODELS_DIR / "eta_residual.pt", weights_only=False)
    model = ResidualETA()
    model.load_state_dict(ckpt["state_dict"])
    return model.eval(), Prep(**ckpt["prep"])


def export() -> dict:
    model, prep = load()
    sample = tensors(pl.read_parquet(ETA_DIR / "test.parquet").head(1024), prep, "cpu")
    args = tuple(sample[k] for k in INPUTS)
    torch.onnx.export(
        model,
        args,
        str(ONNX_PATH),
        input_names=INPUTS,
        output_names=["eta_s"],
        dynamic_axes={k: {0: "batch"} for k in [*INPUTS, "eta_s"]},
        opset_version=17,
        dynamo=False,
    )
    # The exported graph must compute the same numbers as PyTorch.
    sess = ort.InferenceSession(str(ONNX_PATH), providers=["CPUExecutionProvider"])
    feeds = {k: sample[k].numpy() for k in INPUTS}
    got = sess.run(None, feeds)[0]
    want = model(*args).detach().numpy()
    return {
        "onnx": str(ONNX_PATH.name),
        "max_abs_diff_s": float(np.abs(got - want).max()),
        "size_mb": round(ONNX_PATH.stat().st_size / 1e6, 2),
    }


def _time(fn, reps: int) -> dict[str, float]:
    for _ in range(max(3, reps // 10)):  # warm-up: first calls allocate and compile
        fn()
    ts = []
    for _ in range(reps):
        t0 = time.perf_counter()
        fn()
        ts.append((time.perf_counter() - t0) * 1000)
    a = np.asarray(ts)
    return {"p50_ms": float(np.median(a)), "p99_ms": float(np.quantile(a, 0.99))}


def benchmark(batches=(1, 32, 256, 1024, 8192), reps: int = 200) -> dict:
    model, prep = load()
    test_df = pl.read_parquet(ETA_DIR / "test.parquet")
    cpu = tensors(test_df.head(max(batches)), prep, "cpu")
    sess = ort.InferenceSession(str(ONNX_PATH), providers=["CPUExecutionProvider"])
    mps = None
    if torch.backends.mps.is_available():
        mps = {k: v.to("mps") for k, v in cpu.items()}
        model_mps = ResidualETA().to("mps").eval()
        model_mps.load_state_dict(model.state_dict())

    rows = []
    for b in batches:
        sl = {k: v[:b] for k, v in cpu.items()}
        feeds = {k: sl[k].numpy() for k in INPUTS}
        with torch.no_grad():
            runs = {
                "pytorch_cpu": lambda sl=sl: model(*(sl[k] for k in INPUTS)),
                "onnxruntime_cpu": lambda feeds=feeds: sess.run(None, feeds),
            }
            if mps is not None:
                ms = {k: v[:b] for k, v in mps.items()}

                def run_mps(ms=ms):
                    out = model_mps(*(ms[k] for k in INPUTS))
                    torch.mps.synchronize()  # GPU work is async: wait, or we time nothing
                    return out

                runs["pytorch_mps"] = run_mps
            for name, fn in runs.items():
                r = _time(fn, reps)
                rows.append(
                    {"runtime": name, "batch": b, **r, "rows_per_s": b / (r["p50_ms"] / 1000)}
                )
    # Accuracy of the ONNX path on the full test set (should equal PyTorch's).
    te = tensors(test_df, prep, "cpu")
    y = te["y"].numpy()
    onnx_pred = np.concatenate(
        [
            sess.run(None, {k: te[k][i : i + 65536].numpy() for k in INPUTS})[0]
            for i in range(0, len(y), 65536)
        ]
    )
    return {
        "machine": f"{platform.machine()} {platform.processor() or ''}".strip(),
        "torch": torch.__version__,
        "onnxruntime": ort.__version__,
        "test_mae_pytorch_s": float(np.abs(predict(model, te) - y).mean()),
        "test_mae_onnx_s": float(np.abs(onnx_pred - y).mean()),
        "latency": rows,
    }


def write(export_info: dict, bench: dict) -> str:
    dest = RESULTS_DIR / "e6_inference_mac.json"
    dest.write_text(json.dumps({"export": export_info, **bench}, indent=2) + "\n")
    return str(dest)
