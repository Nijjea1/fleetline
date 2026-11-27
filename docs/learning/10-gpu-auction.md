# 10 · A GPU assignment solver: when does the GPU beat the CPU? (E4 GPU column)

## The question
Fleetline's Go solver (successive shortest paths, note 09) matches 100k riders to 200k cars in 168 ms on the M5 Mac. GPUs have thousands of cores. Can a CUDA solver on an RTX 5060 beat it, and if so, above what batch size?

## The algorithm: auction (Bertsekas)
Think of an auction house where **riders bid for cars**:
- Every car has a **price** (starts at 0). A rider values car j at `-(pickup cost) - price_j`.
- Each round, every rider without a car finds its best and second-best car and **bids** just enough to make them equal, plus a small **ε**. Outbid riders lose their car and bid again next round.
- It stops when everyone has a car (or stays "unmatched", which has a huge fixed cost).

**Why it's GPU-friendly:** in one round all unassigned riders bid *at the same time* (one thread each). Conflicts, where two riders bid on the same car, are settled with atomics: `atomicMax` finds the highest bid per car, then `atomicMin` on the rider id breaks ties deterministically. Compare successive shortest paths, which adds riders one at a time.

**ε-scaling:** a big ε finishes fast but sloppily; a tiny ε is exact but slow (prices creep up by ε per bid). So run phases with ε shrinking by a factor θ (8 here), keeping prices between phases, so each phase only fixes the previous one's sloppiness.

**Exact, not approximate:** auction ends within N·ε of the best total. Costs are rounded to whole milliseconds (like OR-Tools) and multiplied by K = N + 1; the last phase runs at ε = 1, so the error is below K, i.e. under 1 ms, and every real total is a multiple of K. The result is *the* optimum. A duality-gap check after each phase can prove optimality even earlier. The bench checks every GPU total against Go anyway.

## Three things that went wrong (and what they taught)
1. **Kernel launches are expensive.** Version 1 launched 3 kernels per round: ~77 µs per round on WSL2, and 147,000 rounds for 1,000 riders = 12 s. Fix: one **cooperative kernel** per ε-phase with grid-wide barriers (`grid.sync()`) between steps: ~2 µs per round. Lesson: on a GPU, many tiny steps cost launch and sync overhead, not arithmetic.
2. **Rectangular problems break the textbook guarantee.** There are 2× more cars than riders. Forward auction is only provably optimal if every car nobody holds ends at price 0. Two shortcuts failed: zeroing freed cars restarted **price wars**, where prices climb in steps of ε all the way to the unmatched cost (~10⁶ rounds on a 7-rider test). Zeroing only idle cars never converged, because forward auction can only *raise* prices. Fix: solve an equivalent **square** problem by adding one "slack" bidder per car (D-018). Lesson: check an algorithm's assumptions before optimizing it.
3. **Starting ε matters.** Starting at the spread of real costs (instead of the unmatched cost) caused the same price war whenever riders outnumbered nearby cars.

## Results (RTX 5060 Laptop + Ryzen AI 9 HX 370, WSL2; median of 3 seeds; `bench/e4/results/rtx5060_wsl2.json`)
| Riders | Go SSP | SciPy sparse | OR-Tools | **GPU auction** |
| --- | --- | --- | --- | --- |
| 1,000 | 1.6 ms | 4.0 ms | 28.6 ms | 250 ms |
| 10,000 | 32.5 ms | 446 ms | 662 ms | 4.95 s (2.1–9.3 s) |
| 30,000 | 121 ms | 3.89 s | 3.00 s | 22.7 s (12.6–24.1 s) |
| 100,000 | 649 ms | 50.5 s | 18.0 s | **1.18 s** |

- **No crossover with Go.** The GPU loses at every size; at 100k it's 1.8× slower than Go on the same machine.
- **At 100k it beats the general-purpose CPU solvers**: 15× faster than OR-Tools, 43× faster than SciPy (end to end with copies: 1.39–1.56 s).
- **It's not monotonic**: 100k is 19× faster than 30k. The seed-to-seed spread at 10k–30k is large (up to 4.4× between seeds at 10k).

## Why: what the profile showed (`bench/e4/profiles/`)
In-kernel counters split each phase into "whole grid bidding" and "the tail", where ≤ 1,024 bidders (riders or slack bidders) are still active and one block of 256 threads finishes alone (cheaper barriers):

| Riders | Rounds (grid / tail) | Tail share of GPU cycles | Bids per round |
| --- | --- | --- | --- |
| 1,000 | 24 / 129,350 | 99.9% | ~4 |
| 30,000 | 1,654 / 6.0 M | 99.8% | ~5 |
| 100,000 | 3,409 / 0.33 M | 93% | ~105 |

Nsight Compute on a mid-run phase agrees. At 30k, SMs are active 4.3% of the time (≈ one of 26 SMs), compute is at 0.17% of peak, memory at 0.36%, and achieved occupancy is 25%. At 100k, 12.4% SM active, 3.7% memory, 65% occupancy.

**The story:** auction's parallelism is "everyone unassigned bids at once", but most of the work is **eviction chains**. Rider A takes B's car, B takes C's, and so on: one step per round, with a handful of riders active. The GPU waits on memory latency for each step, so 25 of its 26 SMs sit idle. At 30k the city has enough cars for 99% of riders, so chains are long and lonely. At 100k only 92% can be matched: competition is everywhere, so each round settles ~100 bids at once and the tail is 18× shorter. The **total work** is similar (28M vs 35M bids); the 100k case just packs it into fewer, fuller rounds.

Successive shortest paths does the *same* eviction chain as one Dijkstra search on one CPU core, with no barriers and good cache locality. Geography keeps those paths short (note 09). That's why Go wins: this problem's structure favors a smart sequential algorithm over brute parallelism.

## What could change the answer
- A **hybrid**: GPU auction with large ε for the parallel early phases, then hand the nearly-solved problem (prices as potentials) to CPU SSP to finish.
- **Forward/reverse auction** for rectangular problems (no slack bidders, half the persons, no zero-cost ties).
- A Gauss–Seidel tail: one warp processes the last few bidders sequentially, without barriers.
- Native Linux instead of WSL2, and a desktop GPU: these change constants, not the shape.

## Interview questions this answers
- "When does a GPU *not* help?" (Low parallelism per step; latency-bound dependent chains; launch/sync overhead.)
- "How do you know your GPU code is correct?" (Exactness argument plus a check against an independent solver on every run, and 232 GPU tests, including 200 random tie-heavy cases and the greedy-trap and long-augmenting-path cases.)
- "How did you find the bottleneck?" (Counters inside the kernel, then Nsight Compute: SM active cycles, occupancy, % of peak.)
- "What's a cooperative kernel / why not one kernel per step?"
