"""E7 driver.

    uv run fleetline-routing prepare train|eval     # stream raw JSON -> data/derived/almrrc/<split>.pkl
    uv run fleetline-routing tune                   # pick zone penalties on a training sample
    uv run fleetline-routing eval                   # held-out eval split, once, with the tuned settings

Protocol (same as E2): settings are chosen on training routes only; the 3,072 evaluation
routes are run once with the final settings. Results -> bench/e7/results/.
"""

import argparse
import json
import os
import platform
import random
import statistics as st
import time
from multiprocessing import Pool

from . import data, solve
from .official import route_score

OUT = data.REPO / "bench/e7/results"
TUNE_GRID = {
    "shortest": None,
    "zones_flat_300": (300, 300, 300),
    "zones_flat_1000": (1000, 1000, 1000),
    "zones_flat_3000": (3000, 3000, 3000),
    "zones_flat_10000": (10000, 10000, 10000),
    "zones_flat_30000": (30000, 30000, 30000),
    "zones_graded_60_600_3000": (60, 600, 3000),
    "zones_graded_300_1000_3000": (300, 1000, 3000),
    "zones_graded_100_1000_10000": (100, 1000, 10000),
}

_routes: dict = {}


def _init(split):
    global _routes
    _routes = data.load(split)


def _run(job):
    rid, name, pen, limit = job
    r = _routes[rid]
    t0 = time.perf_counter()
    order = solve.shortest(r, limit) if pen is None else solve.zones(r, solve.ZonePenalty(*pen), limit)
    solve_s = time.perf_counter() - t0
    return {
        "route": rid,
        "config": name,
        "quality": r.quality,
        "stops": r.n,
        "score": route_score(r, order),
        "tour_s": data.tour_time(r, order),
        "actual_tour_s": data.tour_time(r, r.actual),
        "solve_s": solve_s,
    }


def _sweep(split, ids, configs, limit, workers):
    jobs = [(rid, name, pen, limit) for name, pen in configs.items() for rid in ids]
    t0 = time.time()
    with Pool(workers, initializer=_init, initargs=(split,)) as pool:
        rows = []
        for i, row in enumerate(pool.imap_unordered(_run, jobs, chunksize=4)):
            rows.append(row)
            if (i + 1) % 200 == 0:
                print(f"  {i + 1}/{len(jobs)} routes solved, {time.time() - t0:.0f} s", flush=True)
    return rows


def _summarize(rows):
    out = {}
    for name in dict.fromkeys(r["config"] for r in rows):
        rs = [r for r in rows if r["config"] == name]
        out[name] = {
            "routes": len(rs),
            "score_mean": st.mean(r["score"] for r in rs),
            "score_median": st.median(r["score"] for r in rs),
            "tour_vs_driver": st.mean(r["tour_s"] / r["actual_tour_s"] for r in rs),
            "solve_s_mean": st.mean(r["solve_s"] for r in rs),
            "by_quality": {
                q: st.mean(r["score"] for r in rs if r["quality"] == q) for q in sorted({r["quality"] for r in rs})
            },
        }
    return out


def _env(limit, workers):
    return {
        "machine": platform.platform(),
        "python": platform.python_version(),
        "time_limit_s": limit,
        "workers": workers,
        "when": time.strftime("%Y-%m-%d"),
    }


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("prepare")
    p.add_argument("split", choices=["train", "eval"])
    t = sub.add_parser("tune")
    t.add_argument("--sample", type=int, default=200)
    t.add_argument("--seed", type=int, default=0)
    t.add_argument("--only", default="", help="comma-separated configs to (re)run; merged into tune.json")
    e = sub.add_parser("eval")
    for x in (t, e):
        x.add_argument("--time-limit", type=float, default=1.0)
        x.add_argument("--workers", type=int, default=int(os.environ.get("JOBS", "2")))
    e.add_argument("--configs", default="shortest,best", help="'best' = lowest training score from tune.json")
    a = ap.parse_args()

    if a.cmd == "prepare":
        print("->", data.prepare(a.split))
        return
    OUT.mkdir(parents=True, exist_ok=True)
    if a.cmd == "tune":
        ids = sorted(data.load("train"))
        ids = random.Random(a.seed).sample(ids, a.sample)
        grid = {k: TUNE_GRID[k] for k in a.only.split(",")} if a.only else TUNE_GRID
        rows = _sweep("train", ids, grid, a.time_limit, a.workers)
        summary = _summarize(rows)
        if a.only and (OUT / "tune.json").exists():
            prev = json.loads((OUT / "tune.json").read_text())
            assert prev["sample"] == a.sample and prev["seed"] == a.seed, "merge needs the same training sample"
            summary = {**prev["summary"], **summary}
        best = min((k for k in summary if k != "shortest"), key=lambda k: summary[k]["score_mean"])
        res = {
            "env": _env(a.time_limit, a.workers),
            "sample": a.sample,
            "seed": a.seed,
            "grid": {k: TUNE_GRID[k] for k in summary},
            "best": best,
            "summary": summary,
        }
        (OUT / "tune.json").write_text(json.dumps(res, indent=1) + "\n")
        for k, v in summary.items():
            print(f"{k:32s} score {v['score_mean']:.4f}  tour/driver {v['tour_vs_driver']:.3f}")
        print("best:", best)
    else:
        tune = json.loads((OUT / "tune.json").read_text())
        names = [tune["best"] if c == "best" else c for c in a.configs.split(",")]
        configs = {n: TUNE_GRID[n] for n in names}
        ids = sorted(data.load("eval"))
        rows = _sweep("eval", ids, configs, a.time_limit, a.workers)
        res = {
            "env": _env(a.time_limit, a.workers),
            "tuned_on": "train sample, see tune.json",
            "summary": _summarize(rows),
        }
        (OUT / "eval.json").write_text(json.dumps(res, indent=1) + "\n")
        with open(OUT / "eval_routes.jsonl", "w") as f:
            f.writelines(json.dumps(r) + "\n" for r in rows)
        for k, v in res["summary"].items():
            print(f"{k:32s} score {v['score_mean']:.4f}  tour/driver {v['tour_vs_driver']:.3f}")


if __name__ == "__main__":
    main()
