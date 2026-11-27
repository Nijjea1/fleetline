"""Parse ml/results/e6_tensorrt_raw.txt (from bench.sh) into ml/results/e6_tensorrt.json.

Per batch x precision: TensorRT's GPU compute time (median, p99, mean) and throughput,
in the same spirit as e6_inference_mac.json (p50_ms, p99_ms, rows_per_s), plus the
environment lines bench.sh printed.
"""

import json
import re
from pathlib import Path

RESULTS = Path(__file__).resolve().parents[1] / "results"
raw = (RESULTS / "e6_tensorrt_raw.txt").read_text().splitlines()

env, rows, cur = [], [], None
section = None
for line in raw:
    if line.startswith("== env"):
        section = "env"
        continue
    m = re.match(r"== batch (\d+) (\w+)", line)
    if m:
        section = "bench"
        cur = {"runtime": f"tensorrt_{m.group(2)}", "batch": int(m.group(1))}
        rows.append(cur)
        continue
    if section == "env":
        env.append(line.strip())
    elif cur is not None and "GPU Compute Time: min" in line:
        stats = dict(re.findall(r"(min|max|mean|median|percentile\(99%\)) = ([\d.]+) ms", line))
        cur.update(
            p50_ms=float(stats["median"]),
            p99_ms=float(stats["percentile(99%)"]),
            mean_ms=float(stats["mean"]),
            min_ms=float(stats["min"]),
        )
    elif cur is not None and line.lstrip("[").find("Throughput:") >= 0:
        qps = float(re.search(r"Throughput: ([\d.]+) qps", line).group(1))
        cur["batches_per_s"] = qps
        cur["rows_per_s"] = qps * cur["batch"]

gpu = env[0].split(", ") if env else []
out = {
    "env": {
        "gpu": gpu[0] if gpu else None,
        "driver": gpu[1] if len(gpu) > 1 else None,
        "cuda_toolkit": env[1].removeprefix("cuda ") if len(env) > 1 else None,
        "tensorrt": re.search(r"TensorRT v(\d+)", " ".join(env)).group(1) if env else None,
        "os": "Ubuntu 24.04 on WSL2",
    },
    "method": "trtexec, warm-up 500 ms, >= 2000 iterations and >= 5 s, "
    "GPU compute time only (no host<->device copies); "
    "fp32 = --noTF32, tf32 = TensorRT default, fp16 = FP16 ONNX graph (ml/tensorrt/to_fp16.py)",
    "latency": rows,
}
missing = [r for r in rows if "p50_ms" not in r]
if missing or not rows:
    raise SystemExit(f"no timing parsed for {missing}")
dest = RESULTS / "e6_tensorrt.json"
dest.write_text(json.dumps(out, indent=2) + "\n")
print(f"-> {dest}: {len(rows)} rows")
