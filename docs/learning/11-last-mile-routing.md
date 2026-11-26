# 11 · Last-mile routing: does the optimizer drive like a real driver? (E7)

(Note 10 is reserved for the GPU auction, built on the RTX 5060 machine.)

## The question
Dispatch (notes 04, 09) decides *which* car serves a rider. Delivery has the opposite shape: one van, 100–200 stops, and the decision is the **order**. Textbook answer: the shortest tour (a traveling salesman problem, TSP). But real drivers know things the travel-time matrix doesn't: parking, building access, which side of the street. So the real question is how close an optimizer gets to the order **real Amazon drivers** actually drove, and what simple rule closes the gap.

## The data (`routing/`)
The **Amazon Last Mile Routing Research Challenge** (2021, CC BY-NC 4.0, public S3 bucket) has 9,184 real routes from 5 US metros. Each route has stops with a **zone id** (like `A-2.2C`), a full stop-to-stop travel-time matrix, and the driver's actual sequence.
- 6,112 training routes and 3,052 evaluation routes. Training routes have a quality label (High/Medium/Low); evaluation routes don't.
- The travel-time files are large JSON (2.1 GB train). `data.py` **streams** them with `ijson` instead of loading the whole file, and caches each split as a pickle with float32 matrices.

## The metric (official, not mine)
I use the challenge's own `score.py` (MIT-CAVE rc-cli), downloaded at run time and not modified. Score = **sequence deviation × edit distance weighted by travel time** (ERP), averaged over routes. 0 means the driver's exact order; lower is better.
- Using the official scorer avoids the classic trap of inventing a metric your method happens to be good at.
- Quirk found by a unit test: an **exactly reversed** order also scores 0. So the test checks a shuffle instead.

## The solver
OR-Tools routing: build the tour greedily (`PATH_CHEAPEST_ARC`), then improve it with **guided local search** for 1 s per route. Guided local search is a metaheuristic: when local moves (swap or reverse a segment) stop improving the tour, it penalizes features of the current local optimum (long edges it keeps using) so the search is pushed elsewhere. It doesn't prove optimality, but on 100–200 stops it gets close in a second.

## The idea: finish one zone before starting the next
Drivers work a neighborhood at a time. To model that, add a **penalty** to every edge that leaves a zone. The solver still minimizes time, but crossing zones costs extra, so it only crosses when it has to. The penalty goes into the cost matrix only; tour times are always reported in real seconds.

## Tuning (200 training routes; `bench/e7/results/tune.json`)
| Config | Score | Tour time vs driver |
| --- | --- | --- |
| Shortest tour | 0.0932 | 0.903 |
| Zone penalty 300 s | 0.0584 | |
| Zone penalty 1,000 s | 0.0579 | |
| **Zone penalty 3,000 s** | **0.0565** | 0.933 |
| Zone penalty 10,000 / 30,000 s | 0.0567 / 0.0566 | |
| Graded by zone-id distance (3 variants) | 0.083–0.085 | 1.02–1.03 |

- The score **plateaus** at about 3,000 s: past that point the penalty just means "never leave a zone early", and making it larger changes nothing.
- **A hypothesis that failed:** I guessed zone ids encode adjacency (`A-2.2C` next to `A-2.2D`), so a smaller penalty for nearby ids should help. It made things worse: scores were back near the plain shortest tour, and tours got *longer* than the drivers'. Zone ids aren't a reliable map, so the flat penalty won.

## Held-out result (3,052 evaluation routes, run once; `bench/e7/results/eval.json`)
| | Score (mean) | Median | Tour time vs driver | Better on |
| --- | --- | --- | --- | --- |
| Shortest tour | 0.0877 | 0.0749 | 0.904 | |
| **Zone penalty 3,000 s** | **0.0547 (−37.6%)** | **0.0489** | **0.940** | **2,639 / 3,052 routes (86%)** |

- The penalty was chosen on training routes only. The evaluation set ran once, after tuning was committed.
- One habit (finish the zone) explains more than a third of the gap between "fastest" and "what drivers do". The routes stay **6% shorter** than the drivers' actual routes.
- Solve time: 1.0 s per route (the time limit), on 2 cores.

## Caveats
- The score isn't comparable to the 2021 leaderboard. That leaderboard used a hidden final set, and the top teams also learned from the sequences.
- "Shorter than the driver" uses the challenge's travel-time matrix, which doesn't know about parking or access. It doesn't mean the driver was wrong.

## Interview questions this answers
- "How would you solve a TSP with 150 stops in a second?" (construction heuristic + guided local search)
- "Shortest isn't what users do. How do you encode human preference into an optimizer?" (penalties in the cost function)
- "Tell me about a hypothesis that didn't work." (zone-id adjacency)
- "How do you stream a multi-GB JSON file in Python?"
