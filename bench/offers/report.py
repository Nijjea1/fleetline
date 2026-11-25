"""D-017 validation + E2b report (held-out Sep 23): writes RESULTS.md.

    python3 bench/offers/report.py
"""

import json
import statistics as st
from pathlib import Path

HERE = Path(__file__).parent
E2 = HERE.parent / "e2" / "runs"


def load(path):
    return json.loads((path / "summary.json").read_text())["summary"]


def hour_fit(s):
    """Mean |sim p50 - real p50| / real p50 over measured hours, and hours within 15%."""
    errs = [abs(h["sim"]["p50_s"] - h["real"]["p50_s"]) / h["real"]["p50_s"]
            for h in s["by_hour"] if h["real"]["p50_s"] > 0 and h["sim"]["n"] > 0]
    return st.mean(errs), sum(e <= 0.15 for e in errs), len(errs)


def med(xs):
    return st.median(xs)


def main():
    seeds = (7, 8, 9)
    sets = {
        "greedy, no offers (E2)": [load(E2 / f"09-23_greedy_w1_s{s}") for s in seeds],
        "greedy + offers": [load(HERE / "runs" / f"09-23_greedy_offers_s{s}") for s in seeds],
        "batched, no offers (E2)": [load(E2 / f"09-23_batched_w1_s{s}") for s in seeds],
        "batched + offers": [load(HERE / "runs" / f"09-23_batched_offers_s{s}") for s in seeds],
    }
    real = sets["greedy + offers"][0]["real_wait"]
    out = ["# Driver offers (D-017) and E2b: held-out Sep 23", "",
           "Offer model fitted on Sep 16 only (`calibrate.sh`, `calib/`): 10 s answer delay, decline probability "
           "0.10 + 0.04 per pickup minute, supply re-fitted with it. Median of seeds 7–9.", "",
           "## Does the simulator match real hourly waits better? (greedy, the calibrated baseline)", "",
           "| Setup | Mean per-hour p50 error | Hours within 15% | Day p50 / p90 (s) |", "| --- | --- | --- | --- |"]
    for name in ("greedy, no offers (E2)", "greedy + offers"):
        fits = [hour_fit(s) for s in sets[name]]
        ss = sets[name]
        out.append(f"| {name} | {med([f[0] for f in fits]):.1%} | {med([f[1] for f in fits]):.0f} / {fits[0][2]} | "
                   f"{med([s['sim_wait']['p50_s'] for s in ss]):.0f} / {med([s['sim_wait']['p90_s'] for s in ss]):.0f} |")
    out.append(f"| real Uber/Lyft | | | {real['p50_s']:.0f} / {real['p90_s']:.0f} |")
    out += ["", "## E2b: does batched still beat greedy when drivers decline?", "",
            "| Setup | Mean wait (s) | p50 | p90 | Empty drive (s) | Unserved | Decline rate |",
            "| --- | --- | --- | --- | --- | --- | --- |"]
    m = {}
    for name, ss in sets.items():
        m[name] = {k: med([f(s) for s in ss]) for k, f in {
            "mean": lambda s: s["sim_wait"]["mean_s"], "p50": lambda s: s["sim_wait"]["p50_s"],
            "p90": lambda s: s["sim_wait"]["p90_s"], "pickup": lambda s: s["pickup_eta"]["mean_s"],
            "unserved": lambda s: 100 * s["unserved_rate"], "decl": lambda s: 100 * s.get("decline_rate", 0)}.items()}
        r = m[name]
        out.append(f"| {name} | {r['mean']:.0f} | {r['p50']:.0f} | {r['p90']:.0f} | {r['pickup']:.0f} | "
                   f"{r['unserved']:.2f}% | {r['decl']:.1f}% |")
    out.append("")
    for tag in ("no offers (E2)", "+ offers"):
        g = m[f"greedy, {tag}" if "no" in tag else f"greedy {tag}"]
        b = m[f"batched, {tag}" if "no" in tag else f"batched {tag}"]
        out.append(f"- **{tag}**: batched vs greedy mean wait {100 * (b['mean'] / g['mean'] - 1):+.1f}%, "
                   f"p90 {100 * (b['p90'] / g['p90'] - 1):+.1f}%, empty drive {100 * (b['pickup'] / g['pickup'] - 1):+.1f}%")
    (HERE / "RESULTS.md").write_text("\n".join(out) + "\n")
    print("\n".join(out))


if __name__ == "__main__":
    main()
