# 09 · Batched matching: does seeing everyone at once beat first-come-first-served? (E2/E3)

## The question
Greedy dispatch gives each rider the nearest free car the moment they ask. It's fast and simple, but **myopic**: it can hand rider 1 the only car rider 2 could use, when rider 1 had another option. Batched dispatch collects the waiting riders and solves them together as an **assignment problem**: pick rider↔car pairs, each car used once, minimizing total pickup time.

## The solver (`core/internal/assign`)
- **Sparse, not dense.** Each rider only links to their ~8 nearest cars (per kind), so a round is a few hundred riders × a few thousand cars with ~8 edges per rider. A dense Hungarian matrix would be mostly "impossible" entries.
- **Successive shortest paths.** Add riders one at a time. To fit a new rider in, find the cheapest *augmenting path*: new rider → car X (taken by rider A) → A moves to car Y (taken by B) → B moves to a free car. Dijkstra finds it. Moving A off car X "refunds" A's cost, a negative edge, which Dijkstra can't handle; **node potentials** (Johnson's trick) re-weight every edge to be non-negative without changing which path is shortest.
- **"Unmatched" is an option** with a huge cost, so the solver matches as many riders as possible first, then minimizes time.
- **Proof it's right:** it matches brute force on 2,000 random problems (with ties, and with cheap "unmatched" so leaving riders free is sometimes optimal). A realistic round (300 riders × 8 cars) solves in ~0.1 ms.
- **Fair comparison:** batched reuses greedy's candidate search, radius and pickup cap; only the matching step differs. The greedy refactor was checked byte-identical first (D-013).

## The fairness trap
Minimizing total pickup time alone is unfair under shortage. When not every rider can be matched, a new rider 50 s from a car beats one who waited 8 minutes but is 580 s away, round after round, until that rider gives up. Greedy avoids this by accident (oldest first). Fix: **age weight**. Each second a rider has waited is worth *w* seconds of pickup time.

- *w* = 1 fixed it on one test hour but not over a full day (unserved 0.10% → 0.27%). Pickups can take up to 600 s and riders give up after 600 s, so *w* must be well above 1.
- Tuned on **Sep 16 only**: *w* = 3 (unserved 0.12%, about greedy's). Higher weights give back the wait gains.
- **The held-out day was protected:** the sweep was stopped before any Sep 23 batched result existed; Sep 23 ran once, with *w* = 3. Tuning on the data you report is how benchmarks lie.

## Results (full simulated days, 3 seeds; `bench/e2/RESULTS.md`)
| | Mean wait | Median | p90 | Empty drive to pickup | Unserved |
| --- | --- | --- | --- | --- | --- |
| Sep 23 (held out), greedy | 274 s | 215 s | 597 s | 243 s | 0.07% |
| Sep 23 (held out), batched | **223 s (−18.6%)** | **175 s (−18.4%)** | **503 s (−15.7%)** | **195 s (−19.5%)** | 0.10% |

Seed-to-seed spread is ±2 s, far smaller than the gap. The mechanism shows in the last column: batching cuts the **empty drive** to each rider by ~20%, so each car is busy for less time per trip and more cars are free for the next riders. A small cost remains: +0.03 points of unserved riders.

## E3: the window barely matters (here)
Running rounds every 1, 2, 5 or 10 s changes batched by < 2%. Greedy *improves* with longer windows (mean 276 → 261 s on Sep 23), because a longer window is a crude batch too: more riders and more freed cars per round. So the gap narrows from −18.8% to −16.2% at 10 s. With 7–15 requests per second and many still-waiting riders, a 1 s round already has a big batch; on a sparse day or a small city, the window would matter more.

## Caveats
Absolute waits aren't claimed to match reality hour by hour (D-015: no accept/decline model). The claim is **relative**: same simulator, same day, same supply; only the policy changes.

## Interview questions this answers
- "Greedy vs optimal matching: when does greedy fail?" / "How do you solve assignment at scale?"
- "Your optimizer starved some users. How did you notice and fix it?"
- "How do you avoid overfitting a parameter to your benchmark?"
- "Does a longer batching window always help?"
