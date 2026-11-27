# 13 · NVIDIA cuOpt: solving the assignment as a linear program (E4, cuOpt column)

## What cuOpt is (and isn't)
cuOpt is NVIDIA's GPU optimization library. It targets **vehicle routing** and **LP / MILP** (linear and mixed-integer programs). It has no "assignment problem" function, so to compare it with our solvers we write the matching as a **linear program**.

## The assignment as an LP
- One variable `x_e` between 0 and 1 for every (rider, car) edge, plus `u_i` per rider for "stays unmatched" (cost: the big 10⁶ penalty).
- Each rider: `sum of its x_e + u_i = 1`. Each car: `sum of its x_e <= 1`.
- Minimize total pickup time.

A linear program allows fractions (half a car). The reason this still works: the constraint matrix is **totally unimodular** (every square sub-matrix has determinant 0, ±1), which guarantees that every **vertex** of the feasible region is all 0s and 1s. Simplex methods walk from vertex to vertex, so they return a real assignment.

## Three ways to solve an LP
| Method | How | Where | Output |
| --- | --- | --- | --- |
| **Simplex** (dual simplex) | walk along edges of the polytope, vertex to vertex | CPU in cuOpt | exact vertex: 0/1 here |
| **Barrier** (interior point) | walk through the *inside*, Newton steps | GPU | a point in the middle of the optimal face: can be fractional when there are ties |
| **PDLP** (primal-dual first-order) | many cheap matrix-vector products: GPU-friendly, scales to huge LPs | GPU | approximate: "optimal" means within a tolerance |

**Crossover** turns an interior/approximate solution into a vertex (a few simplex steps from where it ended).

## What we learned
- "Status: optimal" from a first-order method means "within 10⁻⁴ relative". Our objective includes 10⁶-per-unmatched-rider penalties, so 10⁻⁴ relative is huge: default-tolerance PDLP solutions rounded to garbage. Tightening tolerances to 10⁻¹⁰ fixed it on most instances, but not provably.
- Rounding a not-quite-feasible solution is dangerous: two edges into the same car both had `x` just above 0.5, so the first bench run crashed with "a car is assigned twice". Fix: round greedily (largest `x` first, one per rider and car), and **check every answer against an exact solver**, recording the gap.
- The general-purpose GPU solver pays fixed costs (setup, presolve, about 1 s even for tiny problems) that a special-purpose algorithm doesn't.

## Results (`bench/e4/results/rtx5060_wsl2_cuopt.json`, median of 3 seeds)
| Riders | Go SSP | PDLP (GPU) | PDLP + crossover | Dual simplex (CPU) |
| --- | --- | --- | --- | --- |
| 1,000 | 1.4 ms | 1.12 s | 1.00 s | 68 ms |
| 30,000 | 93.6 ms | 1.83 s | 16.8 s | 11.3 s |
| 100,000 | 792 ms | 3.64 s, not optimal | 210 s | 136 s / 142 s, 3rd seed not optimal |

Go wins by 20× to 2,100×. PDLP's time barely grows (the GPU part works), but it starts ~1 s behind and its answer degrades at 100k. A general-purpose solver can't use what our Go solver exploits: each rider has only 8 candidate cars, and augmenting paths stay short in a city (note 09).

## Interview questions this answers
- "Why does an LP give an integer answer for assignment?" (Total unimodularity → integral vertices; need a vertex solution.)
- "What's the difference between simplex, interior point and PDLP?"
- "How do you use a generic solver safely?" (Verify against an exact reference; tolerances are relative to the objective's scale.)
