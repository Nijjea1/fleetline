# 02 · Data and the replay day

## What we built
- `fleetline-sim download`: streams the TLC parquet to a `.part` file and renames on completion (an interrupted download can never look complete).
- `fleetline-sim prepare`: filters one day, applies counted cleaning rules, places every trip at a seeded random point inside its taxi zone, and adds an H3 cell.

## Numbers from our replay day (Wed 2026-09-16)
| | |
| --- | --- |
| Raw requests | 684,361 (Uber 508,727 · Lyft 175,634) |
| Kept after cleaning | 635,294 (92.83%) |
| Real median wait (request → driver on scene) | 208 s |
| Real p90 wait | 549 s |
| Distinct H3 res-8 pickup cells | 1,243 |

The 208 s / 549 s line is what Uber and Lyft's production dispatchers achieved. Our greedy and batched dispatchers will be compared to it, with the honest caveat that our simulated fleet is not their fleet.

## Concepts

**Parquet.** A columnar file format: each column is stored together and compressed. Reading 3 of 25 columns reads roughly 3/25 of the file. Polars' `scan_parquet` is *lazy*: it builds a query plan and only reads the row groups and columns the filter needs.

**Why we sample inside the polygon instead of using the centroid.** With centroids, every pickup in a zone is at the same point, so "distance to nearest driver" is zero or a jump to the next centroid. Real distances inside a zone matter for matching quality.

**Rejection sampling.** Draw uniform points in the bounding box; keep the ones inside the polygon. Correct (uniform over the shape) and, with numpy + `shapely.contains_xy`, vectorized.

**Projected CRS.** Lon/lat degrees are not equal-area (a degree of longitude shrinks toward the poles). We sample in EPSG:2263 (feet, NY State Plane), then convert to lon/lat.

**Reproducibility.** Rows are sorted into a fixed order and zones iterated in sorted order, so the random number generator is consumed identically every run. Verified: two runs → byte-identical files (same SHA-1).

**Counted cleaning.** Every rule prints how many rows it removed. 13,740 trips had the driver "on scene" before the request, a data quirk we drop rather than guess about. If an interviewer asks "how did you clean it?", the answer is a table, not "I removed outliers".

**H3.** Uber's hexagonal grid. Hexagons have uniform neighbor distances (squares have diagonal neighbors farther away), which is why ride-hail uses them for supply/demand bucketing. Resolution 8 ≈ 0.74 km² cells.

## Interview questions this answers
- "Your data has no coordinates — how do you compute distances?" → seeded within-zone sampling; centroids would fake the distances.
- "How do you know your experiment is reproducible?" → same seed, byte-identical inputs, checksum.
