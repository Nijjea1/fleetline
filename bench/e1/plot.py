"""E1 chart: offered vs achieved ingest rate and ingest latency, from bench/e1/results/*.json.

    uv run --project ml python bench/e1/plot.py baseline [after_fix ...]

Each point is the median of the repetitions at that rate; the band is min..max, so the
spread across runs is visible (one lucky run can't make the chart).
"""

import json
import sys
from collections import defaultdict
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402

HERE = Path(__file__).parent
COLORS = ["#2a6fdb", "#e8743b", "#19a979", "#945ecf"]


def load(label):
    d = json.loads((HERE / "results" / f"{label}.json").read_text())
    by = defaultdict(list)
    for s in d["steps"]:
        by[s["offered_rate"]].append(s)
    return d, dict(sorted(by.items()))


def stat(steps, key):
    v = sorted(s[key] for s in steps)
    return v[len(v) // 2], v[0], v[-1]


def table(label, by):
    print(f"\n{label}")
    print(f"{'offered':>8} {'applied (med, min-max)':>26} {'p50 ms':>8} {'p99 ms':>8} {'lag end':>9} {'sustained':>9}")
    for rate, steps in by.items():
        a, lo, hi = stat(steps, "applied_per_s")
        p50 = stat(steps, "ingest_p50_ms")[0]
        p99 = stat(steps, "ingest_p99_ms")[0]
        lag = stat(steps, "lag_end")[0]
        ok = sum(s["sustained"] for s in steps)
        print(f"{rate:>8,} {a:>12,.0f} ({lo:,.0f}-{hi:,.0f}) {p50:>8.1f} {p99:>8.1f} {lag:>9,} {ok}/{len(steps):>7}")


def main(labels):
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11, 4.2))
    top = 0
    for i, label in enumerate(labels):
        _, by = load(label)
        table(label, by)
        c = COLORS[i % len(COLORS)]
        x = [r / 1000 for r in by]
        top = max(top, max(by) / 1000)
        a = [stat(s, "applied_per_s") for s in by.values()]
        ax1.plot(x, [m / 1000 for m, _, _ in a], "o-", color=c, label=label)
        ax1.fill_between(x, [lo / 1000 for _, lo, _ in a], [hi / 1000 for _, _, hi in a], color=c, alpha=0.2)
        for q, style in (("ingest_p50_ms", "o-"), ("ingest_p99_ms", "s--")):
            v = [stat(s, q) for s in by.values()]
            # -1 means "beyond the histogram" (> 33 s): draw it at the top edge.
            ax2.plot(x, [m if m >= 0 else 33000 for m, _, _ in v], style, color=c,
                     label=f"{label} {q.split('_')[1]}")
    ax1.plot([0, top], [0, top], ":", color="#888", label="achieved = offered")
    ax1.set(xlabel="offered pings/s (thousands)", ylabel="applied to Redis, pings/s (thousands)",
            title="Throughput")
    ax2.set(xlabel="offered pings/s (thousands)", ylabel="ms (log)", yscale="log",
            title="Ingest latency: broker append → Redis write")
    for ax in (ax1, ax2):
        ax.grid(alpha=0.3)
        ax.legend(fontsize=8, frameon=False)
        ax.spines[["top", "right"]].set_visible(False)
    fig.suptitle("E1 · fleetd ingest on one M5 MacBook (Docker Desktop: Redpanda + Redis)", fontsize=10)
    fig.tight_layout()
    out = HERE / "e1.png"
    fig.savefig(out, dpi=150)
    print(f"\n-> {out}")


if __name__ == "__main__":
    main(sys.argv[1:] or ["baseline"])
