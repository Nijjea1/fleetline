# Roadmap: what's left (as of 2026-11-25)

Phase 1 is done (see `PHASE1_CHECKLIST.md`). This list covers everything else in `PROJECT_SCOPE.md`, in build order.
**Where:** Mac = I can build and run it here · GPU = needs the RTX 5060 machine (I write the code, you run it) · You = your decision or your account.

## Phase 2 · Matching + GPU
| # | Item | Where | Status |
| --- | --- | --- | --- |
| 2.1 | E2: batched vs greedy (held-out Sep 23: mean wait −18.6%) | Mac | ✅ |
| 2.2 | E3: dispatch window 1–10 s | Mac | ✅ |
| 2.3 | E4 CPU: Go SSP vs SciPy vs OR-Tools, 100–100k riders | Mac | ✅ |
| 2.4 | Driver accept/decline + response delay, fitted on Sep 16, checked on Sep 23 (closes the realism gap, D-015) | Mac | ✅ built; realism gain did not hold on Sep 23 (D-017), so off by default |
| 2.5 | E2b: does batched still win when drivers decline? | Mac | ✅ yes: −16.9% mean wait with 24–27% of offers declined |
| 2.6 | Dashboard: greedy vs batched side by side on the same morning | Mac | ⬜ |
| 2.7 | GPU smoke test: `matcher/gpu_smoke/run.sh` | GPU | ⬜ needs you |
| 2.8 | CUDA assignment solver (auction algorithm) + pybind11, joins E4; Nsight profile | GPU | ⬜ code here, run there |
| 2.9 | NVIDIA cuOpt in E4 | GPU (Linux/WSL) | ⬜ |

## Phase 3 · ML + cloud
| # | Item | Where | Status |
| --- | --- | --- | --- |
| 3.1 | E5: ETA model (LightGBM 54% better than OSRM) | Mac | ✅ |
| 3.2 | E6 on Mac: ONNX Runtime CPU vs Apple GPU | Mac | ✅ |
| 3.3 | E6 on NVIDIA: TensorRT FP32 / FP16 / INT8 (script ready in `ml/tensorrt`) | GPU | ⬜ needs you |
| 3.4 | E5b: dispatch with the learned ETA instead of straight-line: does it cut waits? | Mac | ⬜ |
| 3.5 | E7: Amazon Last Mile routes, stop-order match vs real drivers | Mac (multi-GB download) | ⬜ |
| 3.6 | AWS deploy with Terraform (EKS/MSK or a cheaper ECS variant), GitHub Actions deploy | You (AWS account, budget) | ⬜ decide budget |

## Phase 4 · Ship
| # | Item | Where | Status |
| --- | --- | --- | --- |
| 4.1 | Make the repo public (email is in commits; see `RESUME_CONTEXT.md`) | You | ⬜ your call |
| 4.2 | Final README: every chart, every number linked | Mac | ⬜ after 2.x/3.x |
| 4.3 | 2-minute demo video (I write the script and shot list) | You record | ⬜ |
| 4.4 | Write-up blog post | Mac | ⬜ |
| 4.5 | E8 (stretch): Waymo motion model | GPU + accept Waymo terms | ⬜ optional |
| 4.6 | Swap final numbers into `RESUME_CONTEXT.md` | Mac | ongoing |
