# Fleetline — Project Scope

Nov 12, 2026 · Avneet Singh Nijjer

> **For Claude Code:** this file is the source of truth for the Fleetline project. Build in the phase order in the Roadmap section, keep every experiment reproducible in `bench/`, and never put a number in the README or resume that a benchmark did not produce.

## What Fleetline is

Fleetline is a real-time dispatch platform for a mixed fleet of human drivers and robotaxis, with GPU-accelerated matching and ML-predicted arrival times. It replays real city demand, assigns every request to a vehicle within a fixed time budget, and measures whether each change actually makes the city better served.

It exists to give you one deep, measured systems project that matches the core problem at Uber, DoorDash, Tesla, Waymo, NVIDIA and Amazon. Each of those companies is solving some version of "move things through a city fast, with predictions, at scale." The project is built like your Ampliphi shadow-mode work: real data, a baseline, one change at a time, and honest numbers.

**The one-line pitch:** "I built a dispatch engine that matches riders to a mixed human and robotaxi fleet in real time, moved the matcher onto the GPU with my own CUDA kernel, and served the ETA model with TensorRT, all benchmarked on real NYC and Amazon delivery data."

## Goals and success criteria

Fleetline succeeds when a recruiter can open a live demo in one click and an interviewer can ask about any layer and get a measured answer. The numbers below are targets to design toward; the real numbers come from the benchmarks and replace these on the resume.

| Goal | Target to aim for | How it is proven |
| --- | --- | --- |
| Ingest live vehicle pings | Tens of thousands of events per second on one node, p99 under 50 ms end to end | Load test with a published script and graph |
| Match fairly and fast | Batched matching beats greedy on average rider wait time | Same replayed day, one variable changed |
| GPU speedup | Your CUDA matcher clearly faster than the CPU version at thousands of riders and drivers | Nsight profile plus a timing table across sizes |
| Better ETAs | ML residual model beats routing-only ETA on mean absolute error | Held-out days of NYC trips |
| Fast inference | TensorRT ETA model serves at sub-millisecond latency per batch | Latency at several batch sizes, FP32 vs FP16 vs INT8 |
| Real delivery data | Your router run on Amazon's 9,184 real routes, compared to the driver's actual sequence | Published comparison notebook |
| Robotaxi layer (stretch) | A ranked submission on the Waymo Open Motion leaderboard | Leaderboard screenshot and link |
| Shippable | Live dashboard URL, clean README, architecture diagram, 2-minute demo video | Anyone can open it without setup |

The project is "done enough to put on the resume" after Phase 1, and gets stronger with each phase after that.

## Architecture

Fleetline is five layers stacked on one data flow: replayed demand enters the Go core, the dispatch service matches it, and GPU models feed better predictions back into each decision.

```
 PUBLIC DATA
 [NYC TLC trips]   [Amazon Last Mile routes]   [Waymo Open Motion]
 real demand        9,184 routes to benchmark    trajectories (stretch)
        |
        v
 1 · STREAMING CORE (Go)
 [Simulator + Go ingest] --> [Kafka, keyed by H3 cell] --> [Redis vehicle state]
                                      |
                                      v
 2 · DISPATCH
 [Optimizer] <-- [DISPATCH SERVICE] --> [Live dashboard]
 OR-Tools ->      matches every 1-10 s    map + metrics, deck.gl
 CUDA -> cuOpt
      ^                  ^                        ^
      |                  |                        |
 3-4 · MODELS (RTX 5060)
 [Motion model]     [ETA service]            [Benchmarks E1-E8]
 Waymo, stretch     PyTorch -> TensorRT, C++  every claim, measured

 5 · AWS: EKS, MSK, DynamoDB, S3, Terraform, CI   (runs everything above)
```

Replayed NYC trips enter the streaming core as live pings, partitioned by H3 hex over Kafka. Every batch window, the dispatch service asks the optimizer for the best assignments, using ETAs from the TensorRT service and, in the stretch layer, robotaxi motion predictions. The dashboard and the benchmarks read every outcome, and the whole stack deploys to AWS.

1. **Streaming core.** Go service, Kafka keyed by H3 cell, Redis for live vehicle state.
2. **Dispatch optimizer.** Greedy baseline, then batched OR-Tools, then your CUDA kernel, then cuOpt for multi-stop routes.
3. **ETA model.** A PyTorch residual model on top of OSRM road times, exported to TensorRT and served from C++.
4. **Robotaxi layer (stretch).** A motion model trained on Waymo data that flags risky pickups for autonomous vehicles.
5. **AWS deployment.** EKS, MSK, DynamoDB and S3 via Terraform, plus the Amazon routing benchmark.

## How it maps to each company

Every target company sees at least two layers that look like their own work. The "what they work on" column comes from their job postings and engineering blogs (see Sources).

| Company | What they work on | Fleetline layers that match | What you can say in the interview |
| --- | --- | --- | --- |
| Uber | Matching as a long-term marketplace problem; ML that corrects routing-based ETAs | Streaming core, dispatch optimizer, ETA model | "I showed greedy matching hurts later wait times and measured how much batching fixes it." |
| DoorDash | ML predictions feeding a mixed-integer optimizer; simulation before launch; batch rate trade-offs | Dispatch optimizer, simulator, experiments | "I replay a real day and change one knob, like the batch window, to see the trade-off." |
| NVIDIA | CUDA, TensorRT, real-time inference, open-source cuOpt for routing | Custom CUDA kernel, cuOpt, TensorRT serving | "I wrote a CUDA kernel for the matcher, profiled it in Nsight, and compared it to cuOpt." |
| Tesla | Autopilot core infra: distributed jobs, Go, Kubernetes, concurrency; inference on custom hardware | Go streaming core on Kubernetes, quantized inference | "My Go service handles concurrent pings with backpressure; I measured accuracy lost to INT8." |
| Waymo | Perception and motion prediction on the Waymo Open Dataset | Robotaxi layer, trajectory model | "I trained a motion model on WOMD and used it to plan safer robotaxi pickups." |
| Amazon | Scalable AWS services, last-mile routing | AWS deployment, Amazon Last Mile benchmark | "I benchmarked my router on Amazon's own 9,184 delivery routes." |
| Meta | DSA-heavy loops; PyTorch | ETA model in PyTorch | Mostly a coding-interview win; the project is the shipped-project line on the resume |
| Google | Distributed systems; new project-design discussion round; OR-Tools | OR-Tools CPU baseline, full system design story | "Here's the architecture, the bottleneck I found, and how I measured the fix." |

The strongest overlap is Uber, DoorDash and NVIDIA. Tesla and Amazon come from the infrastructure layers. Waymo depends on the stretch layer.

## Tech stack and hardware

The RTX 5060 machine does all GPU work; the Mac does the Go core, the dashboard and the AWS infrastructure. Both push to one GitHub monorepo.

| Component | Technology | Runs on | Why this choice |
| --- | --- | --- | --- |
| Simulator and event replay | Python (Polars), later Go | Either | Fast to build; replays NYC trips as timed events |
| Streaming core | Go, Kafka (Redpanda locally), Redis, H3 | Mac, then AWS | Go and concurrency are named in Tesla infra postings; H3 is Uber's grid |
| CPU optimizer baseline | Google OR-Tools, SciPy Hungarian | Either | A credible, named baseline to beat |
| GPU matcher | CUDA C++ kernel, pybind11 wrapper | RTX 5060 | Your own kernel is the NVIDIA talking point |
| GPU routing | NVIDIA cuOpt | RTX 5060 | Open source, built for dispatch and VRP |
| ETA model | PyTorch, ONNX export, TensorRT, C++ server | RTX 5060 | Train in PyTorch, serve fast with TensorRT |
| Road network | OpenStreetMap + OSRM | Docker | Real road travel times for the routing baseline |
| Robotaxi model (stretch) | PyTorch on Waymo Open Motion Dataset | RTX 5060 | The Waymo and Autopilot talking point |
| Deployment | AWS EKS, MSK, DynamoDB, S3, Terraform, GitHub Actions | AWS | Amazon's own stack; reuses your OIDC and CI experience |
| Dashboard | Next.js + deck.gl map | Vercel | Live one-click demo |
| Observability | Prometheus + Grafana | Docker / AWS | Every latency claim has a graph behind it |

**Setting up the RTX 5060 machine**

- **Use Linux.** Native Ubuntu (dual-boot) is the smoothest path for CUDA, Nsight and cuOpt. WSL2 on Windows works for most CUDA work, but check each tool's support page before relying on it.
- **Install a recent CUDA toolkit and driver.** The 5060 is a new Blackwell-generation card, so older CUDA versions and older PyTorch builds may not support it. Install current versions and run a test kernel first.
- **Plan around 8 GB of VRAM.** Fine for the CUDA matcher, cuOpt and the ETA model. The Waymo motion model needs a small architecture, a data subset and mixed precision; rent a cloud GPU only if a run won't fit.
- **Install Nsight Systems and Nsight Compute** on day one. Profiling screenshots are part of the deliverable.

**Repo layout**

```
fleetline/
  sim/          event replay + scenario configs
  core/         Go ingest, state, matching service
  matcher/      CUDA kernel, pybind11 bindings, CPU baselines
  eta/          PyTorch training, ONNX/TensorRT export, C++ server
  av/           Waymo motion model (stretch)
  bench/        every experiment as a script + results/
  infra/        Terraform, Kubernetes manifests, CI
  web/          Next.js + deck.gl dashboard
  docs/         architecture diagram, write-ups
```

## ML stack

Two ML models carry the project: an ETA model (core) and a robotaxi motion model (stretch). Both train in PyTorch and serve through TensorRT. Each starts from a simple baseline so every gain is measured.

| Model | Baseline first | Main model | Trained on | Served with |
| --- | --- | --- | --- | --- |
| ETA (layer 3) | OSRM road time alone, then LightGBM on trip features | Small PyTorch MLP predicting the residual error of the OSRM time, Uber DeepETA-style | NYC TLC trips + OSM road network | ONNX → TensorRT (FP32, FP16, INT8), C++ server |
| Robotaxi motion (layer 4, stretch) | Constant-velocity prediction | Small vectorized transformer that predicts several possible future paths, sized to fit 8 GB VRAM | Waymo Open Motion Dataset | ONNX → TensorRT |

**Supporting tools:** Polars for data prep, MLflow or Weights & Biases for experiment tracking, OR-Tools and cuOpt for optimization (solvers, not ML).

**Language choice:** Go for the streaming core, CUDA C++ for the GPU matcher and the TensorRT server, Python for training and benchmarks. No Rust: the bottlenecks are the GPU kernel and the network, not the language, and Go and CUDA C++ match the target job postings. Revisit only if profiling shows Go garbage-collection pauses hurting p99 latency.

## Data

All data is public and free. Two of the sets are licensed for non-commercial use only, which fits a portfolio project; check each license before you publish derived data.

| Dataset | What it gives Fleetline | Used in | License note |
| --- | --- | --- | --- |
| NYC TLC trip records | Real pickup and dropoff times and zones, to replay demand | Simulator, ETA training, all matcher benchmarks | Public city data |
| [Amazon Last Mile Routing Research Challenge](https://registry.opendata.aws/amazon-last-mile-challenges) | 9,184 real Amazon delivery routes with stop and package features | Routing benchmark | Non-commercial (CC BY-NC 4.0) |
| [Waymo Open Motion Dataset](https://waymo.com/open/) | Real agent trajectories for motion prediction | Robotaxi layer (stretch) | Non-commercial; accept terms to download |
| OpenStreetMap | Road network for real travel times | OSRM routing baseline | Open (ODbL) |

## Roadmap

Phase 1 is the only must-have before you apply: it puts a live, measured project on the resume in three weeks. Every later phase adds a company-specific talking point.

```
 Phase 1 · Core      Phase 2 · GPU       Phase 3 · ML + AWS   Phase 4 · Polish
 Weeks 1-3           Weeks 4-6           Weeks 7-9            Weeks 10-12
 ------------------> ------------------> -------------------> ----------------->
           [Resume-ready]    [NVIDIA story done]   [Deployed live]        [Ship it]
```

**Phase 1 · Core (weeks 1–3)**

- [ ] Get a CUDA test kernel and PyTorch running on the RTX 5060
- [ ] Download NYC TLC data; build the event replayer
- [ ] Go ingest service, Kafka keyed by H3 cell, Redis vehicle state
- [ ] Greedy nearest-driver dispatch
- [ ] deck.gl dashboard deployed on Vercel
- [ ] Run E1 load test; first README with architecture diagram

**Phase 2 · GPU (weeks 4–6)**

- [ ] Batched matching with OR-Tools and SciPy Hungarian
- [ ] CUDA kernel for the cost matrix and assignment, with pybind11 bindings
- [ ] cuOpt for multi-stop routes
- [ ] Nsight profiles; run E2, E3, E4

**Phase 3 · ML + AWS (weeks 7–9)**

- [ ] OSRM road times; PyTorch residual ETA model
- [ ] ONNX export, TensorRT engine, C++ server; run E5, E6
- [ ] Terraform: EKS, MSK, DynamoDB, S3; GitHub Actions deploy
- [ ] Amazon Last Mile benchmark; run E7

**Phase 4 · Polish (weeks 10–12)**

- [ ] Waymo motion model and leaderboard submission (stretch); run E8
- [ ] Final README with every benchmark chart
- [ ] 2-minute demo video and a write-up blog post
- [ ] Swap the real numbers into the resume bullets

## Experiments

Eight experiments produce every number on the resume. Each lives in `bench/` as one script, writes its results to a file, and gets a chart in the README.

| # | Question | What is measured | Speaks to |
| --- | --- | --- | --- |
| E1 | How much load can the core take? | Events per second, p50/p99 latency, behavior when Kafka backs up | Tesla, Uber, Amazon, Google |
| E2 | Does batching beat greedy? | Average and p90 rider wait, driver idle time, unmatched requests | Uber, DoorDash |
| E3 | How long should the batch window be? | Wait time vs. match quality across 1 to 10 second windows | DoorDash, Uber |
| E4 | When does the GPU win? | Solve time vs. problem size for CPU Hungarian, OR-Tools, your CUDA kernel, cuOpt | NVIDIA |
| E5 | Does ML improve ETAs? | Mean absolute error of routing-only vs. routing plus residual model | Uber, DoorDash |
| E6 | What does fast inference cost? | Latency and accuracy for PyTorch vs. TensorRT FP32, FP16, INT8 | NVIDIA, Tesla |
| E7 | How good is the router on real delivery routes? | Route length and sequence match vs. the Amazon driver's actual route | Amazon |
| E8 | Does motion prediction make robotaxi pickups safer? | Leaderboard metrics; pickups flagged as conflicting with predicted traffic | Waymo, Tesla |

**Rules for every experiment**

- Same replayed day, same seed, one variable changed.
- Run each config several times and report the spread, not one lucky run.
- Keep the losing results too. "The GPU only wins above N riders" is a better interview answer than "the GPU is faster."

## Resume bullets and talking points

These are templates: every [bracket] gets the real number from its experiment, and a bullet only goes on the resume once its number exists. Pick 3 to 4 per application, tuned to the role.

- Built a real-time dispatch engine in Go that ingests [N] vehicle events/sec at [X] ms p99, partitioned by H3 cell over Kafka and deployed on AWS EKS with Terraform. *(E1: Tesla, Uber, Amazon)*
- Cut average rider wait by [X]% vs. greedy matching with batched assignment, measured by replaying [N] real NYC trips. *(E2: Uber, DoorDash)*
- Wrote a CUDA kernel for batched rider-driver assignment, [X]× faster than a CPU Hungarian solver at [N] × [N], profiled with Nsight; benchmarked against NVIDIA cuOpt. *(E4: NVIDIA)*
- Reduced ETA mean absolute error by [X]% with a PyTorch residual model over routing estimates; served via TensorRT at [X] ms per batch of [N], with [X]% accuracy cost at INT8. *(E5, E6: NVIDIA, Uber, Tesla)*
- Benchmarked the routing engine on Amazon's 9,184 real last-mile routes, matching driver stop order [X]% of the time. *(E7: Amazon)*
- Trained a motion prediction model on the Waymo Open Motion Dataset, ranked [#] on the public leaderboard. *(E8 stretch: Waymo, Tesla)*

**Interview stories this project gives you**

1. **The bottleneck story (Google design round, Amazon "Dive Deep").** Where the latency really lived, how you found it in the profile, and what you changed.
2. **The GPU-doesn't-always-win story (NVIDIA).** The problem size where the CUDA kernel overtakes the CPU, and why: launch overhead, memory transfer, occupancy.
3. **The trade-off story (DoorDash, Uber).** Longer batch windows improve matches but make riders wait; how you measured where the sweet spot is.
4. **The measurement-integrity story (all).** Same seed, one variable, the spread across runs. This ties directly back to your Ampliphi shadow-mode work.

## Risks and scope cuts

The biggest risk is building too much and finishing nothing, so the cut order is decided now.

| Risk | What happens | Fallback |
| --- | --- | --- |
| Scope creep | Five half-done layers, no demo | Cut in this order: Waymo layer, then Amazon benchmark, then INT8. Never cut the CUDA kernel or the benchmarks. |
| New GPU, young toolchain | CUDA, PyTorch or cuOpt fails to install on the 5060 | Get a test kernel running in week 1; fall back to a rented cloud GPU for any one tool that won't work |
| 8 GB VRAM | Waymo model won't train at full size | Smaller model, data subset, mixed precision; the leaderboard rank matters less than the write-up |
| AWS cost | A forgotten EKS cluster bills for weeks | Terraform everything, `terraform destroy` after demos, set a billing alarm on day one |
| School and research load | Weeks slip during midterms | Phase 1 is the only must-have before applications; later phases are additive |
| Interview prep crowded out | Great project, failed coding round | 1 to 2 LeetCode mediums a day runs alongside, every week |

**What not to build:** user accounts, payments, a rider mobile app, or a chatbot. None of them help any target company, and each eats weeks.

## Sources

- [NVIDIA 2027 Internships: Software Engineering](https://app.joinrunway.io/explore/job/cmszv5gwu000tb20i2m6ikfk3)
- [NVIDIA: Accelerate decision optimization using open source cuOpt](https://developer.nvidia.com/blog/accelerate-decision-optimization-using-open-source-nvidia-cuopt)
- [Tesla AI Software Engineer Intern, Core Infrastructure](https://peerlist.io/company/tesla88/careers/internship-ai-software-engineer-core-infrastructure-winterspring-2027/jobh6a7qmpekkmd9ncg8pk6nn8rgmr?page=8)
- [Tesla AI inference co-design intern](https://www.dice.com/job-detail/e6e8c3a6-7fdc-4ae7-9777-0f13ec1a89a0)
- [Waymo Open Dataset Challenges](https://waymo.com/intl/it/open/challenges/)
- [DoorDash: Using ML and Optimization to Solve DoorDash's Dispatch Problem](https://careers.doordash.com/?p=5342)
- [DoorDash: Causal ML and joint optimization](https://careersatdoordash.com/?p=16159)
- [Uber: Reinforcement learning for modeling marketplace balance](https://www.uber.com/blog/reinforcement-learning-for-modeling-marketplace-balance)
- [Amazon Last Mile Routing Research Challenge dataset](https://registry.opendata.aws/amazon-last-mile-challenges)
- [Google SWE intern interview guide (Exponent)](https://www.tryexponent.com/guides/google-software-engineer-intern-interview)
- [Meta internship guide (Extern)](https://www.extern.com/post/meta-internship-guide)
