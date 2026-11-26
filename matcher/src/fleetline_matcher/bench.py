"""E4 (CPU part): solve time vs problem size for every assignment solver.

    cd matcher && uv run python -m fleetline_matcher.bench

For each size and seed: build one instance from real NYC points, run every solver, check
that all of them reach the same optimal total (a wrong solver can't be fast by accident),
and record the fastest of a few repetitions. Writes bench/e4/results/<machine>.json and
bench/e4/e4.png. The CUDA solver joins on the RTX 5060 machine.
"""

import argparse
import json
import platform
import subprocess
import tempfile
import time
from pathlib import Path

import numpy as np

from .instances import REPO, Instance, build, objective
from .solvers import CUDA_STATS, ROUNDED, SOLVERS

GO_BIN = REPO / "core/bin/assignbench"
OUT = REPO / "bench/e4"
DENSE_MAX = 3000  # n x 3n float64 matrix: 3k -> 216 MB, 10k -> 2.4 GB


def run_go(inst: Instance, reps: int, tmp: Path):
    f, m = tmp / "inst.bin", tmp / "match.bin"
    inst.save(f)
    out = subprocess.run(
        [str(GO_BIN), "-reps", str(reps), "-match", str(m), str(f)], check=True, capture_output=True, text=True
    )
    t = json.loads(out.stdout)
    return np.fromfile(m, "<i4").astype(int), t["setup_s"], t["solve_s"]


def time_py(fn, inst: Instance, reps: int):
    best = None
    for _ in range(reps):
        match, setup, solve = fn(inst)
        if best is None or solve < best[2]:
            best = (match, setup, solve, dict(CUDA_STATS) if fn is SOLVERS.get("cuda_auction") else {})
    return best


def gpu_env() -> dict:
    """GPU name, driver and CUDA toolkit version, if this machine has them."""

    def out(*cmd):
        return subprocess.run(cmd, check=False, capture_output=True, text=True).stdout.strip()

    env = {}
    cpuinfo = Path("/proc/cpuinfo")
    if cpuinfo.exists():
        env["cpu"] = next(
            (ln.split(":", 1)[1].strip() for ln in cpuinfo.read_text().splitlines() if ln.startswith("model name")), ""
        )
    try:
        env["gpu"] = out(
            "nvidia-smi", "--query-gpu=name,driver_version,memory.total,compute_cap", "--format=csv,noheader"
        )
        env["nvcc"] = out("nvcc", "--version").splitlines()[-1]
    except (OSError, IndexError):
        pass
    return env


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--sizes", default="100,300,1000,3000,10000,30000,100000")
    ap.add_argument("--seeds", type=int, default=3)
    ap.add_argument("--machine", default=f"cpu_{platform.machine()}_{platform.system().lower()}")
    args = ap.parse_args()
    if not GO_BIN.exists():
        raise SystemExit("build it first: cd core && go build -o bin/assignbench ./cmd/assignbench")

    rows = []
    with tempfile.TemporaryDirectory() as tmp:
        for n in map(int, args.sizes.split(",")):
            reps = 5 if n <= 3000 else 2
            for seed in range(args.seeds):
                inst = build(n, seed=seed)
                results = {"go_ssp": run_go(inst, reps, Path(tmp))}
                for name, fn in SOLVERS.items():
                    if name == "scipy_dense" and n > DENSE_MAX:
                        continue
                    results[name] = time_py(fn, inst, reps)
                objs = {name: objective(inst, r[0]) for name, r in results.items()}
                ref = objs["go_ssp"]
                # OR-Tools and the GPU auction round costs to whole milliseconds: allow 1 ms per row.
                for name, v in objs.items():
                    tol = 1e-3 * n if name in ROUNDED else 1e-6 * max(1.0, abs(ref))
                    if abs(v - ref) > tol:
                        raise SystemExit(f"n={n} seed={seed}: {name} total {v} != go_ssp {ref}")
                matched = int((results["go_ssp"][0] >= 0).sum())
                for name, (_, setup, solve, *extra) in results.items():
                    row = {
                        "n": n,
                        "seed": seed,
                        "solver": name,
                        "solve_s": solve,
                        "setup_s": setup,
                        "edges": inst.n_edges,
                        "matched": matched,
                    }
                    if extra and extra[0]:  # GPU: phases, rounds, and how setup splits
                        row["gpu"] = extra[0]
                    rows.append(row)
                line = "  ".join(f"{k} {v[2] * 1e3:9.2f} ms" for k, v in results.items())
                print(f"n={n:>6} seed={seed} edges={inst.n_edges:>7} matched={matched:>6}  {line}", flush=True)

    OUT.joinpath("results").mkdir(parents=True, exist_ok=True)
    meta = {
        "machine": args.machine,
        "platform": platform.platform(),
        "processor": platform.processor(),
        "python": platform.python_version(),
        "when": time.strftime("%Y-%m-%d"),
        **gpu_env(),
    }
    path = OUT / "results" / f"{args.machine}.json"
    path.write_text(json.dumps({"meta": meta, "rows": rows}, indent=1) + "\n")
    print(f"-> {path}")
    plot()


def plot():
    """One line per solver per machine, from every committed result file: solid for the
    first machine (the Mac), dashed for the next (the RTX 5060 box)."""
    import matplotlib.pyplot as plt

    colors = {
        "go_ssp": "#2f6fdb",
        "scipy_sparse": "#3ddc97",
        "scipy_dense": "#f0a35e",
        "ortools_flow": "#c084fc",
        "cuda_auction": "#e5484d",
    }
    styles = ["o-", "s--", "^:"]
    files = sorted(OUT.joinpath("results").glob("*.json"), key=lambda p: (not p.stem.startswith("cpu"), p.stem))
    fig, ax = plt.subplots(figsize=(8, 5))
    for style, f in zip(styles, files):
        rows = json.loads(f.read_text())["rows"]
        for solver, color in colors.items():
            pts = {}
            for r in rows:
                if r["solver"] == solver:
                    pts.setdefault(r["n"], []).append(r["solve_s"] * 1e3)
            if pts:
                ns = sorted(pts)
                med = [float(np.median(pts[n])) for n in ns]
                ax.plot(ns, med, style, color=color, label=f"{solver} · {f.stem}", ms=4)
    ax.set(
        xscale="log",
        yscale="log",
        xlabel="riders in one batch (cars = 2x, 8 nearest each)",
        ylabel="solve time, ms (median of seeds)",
        title="E4 · assignment solve time vs batch size",
    )
    ax.grid(alpha=0.3, which="both")
    ax.legend(frameon=False, fontsize=7, ncol=2)
    ax.spines[["top", "right"]].set_visible(False)
    fig.tight_layout()
    fig.savefig(OUT / "e4.png", dpi=150)
    print(f"-> {OUT / 'e4.png'}")


if __name__ == "__main__":
    main()
