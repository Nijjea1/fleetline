# Driver offers (D-017) and E2b: held-out Sep 23

Offer model fitted on Sep 16 only (`calibrate.sh`, `calib/`): 10 s answer delay, decline probability 0.10 + 0.04 per pickup minute, supply re-fitted with it. Median of seeds 7–9.

## Does the simulator match real hourly waits better? (greedy, the calibrated baseline)

| Setup | Mean per-hour p50 error | Hours within 15% | Day p50 / p90 (s) |
| --- | --- | --- | --- |
| greedy, no offers (E2) | 38.4% | 7 / 23 | 215 / 597 |
| greedy + offers | 35.3% | 4 / 23 | 206 / 608 |
| real Uber/Lyft | | | 208 / 546 |

## E2b: does batched still beat greedy when drivers decline?

| Setup | Mean wait (s) | p50 | p90 | Empty drive (s) | Unserved | Decline rate |
| --- | --- | --- | --- | --- | --- | --- |
| greedy, no offers (E2) | 274 | 215 | 597 | 243 | 0.07% | 0.0% |
| greedy + offers | 275 | 206 | 608 | 229 | 0.07% | 27.4% |
| batched, no offers (E2) | 223 | 175 | 503 | 195 | 0.10% | 0.0% |
| batched + offers | 229 | 178 | 505 | 187 | 0.10% | 24.1% |

- **no offers (E2)**: batched vs greedy mean wait -18.6%, p90 -15.7%, empty drive -19.5%
- **+ offers**: batched vs greedy mean wait -16.9%, p90 -16.9%, empty drive -18.1%
