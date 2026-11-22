# 06 · Pickup caps, skipping wasted work, and calibrating supply

## 1. The wild goose chase, and the pickup cap
With no limit, greedy dispatch hands a waiting rider the nearest idle car **however far away it is**. In a rush hour, "nearest" can be 15 minutes away. That car spends 15 minutes driving empty, so it's unavailable for the riders who appear next to it, who then get *their* car from far away... Supply drains into empty driving. Economists call this the **wild goose chase** (Castillo, Knoepfle & Weyl). Our full-day run showed it: p90 wait **1,370 s** against a real 550 s.

**Fix:** refuse any pickup longer than `MaxPickupSec`, and wait for a closer car instead. It sounds worse for that rider, but the whole system serves more people.

| Run (Sep 16, utilization 0.75) | Unserved | p50 wait | p90 wait |
| --- | --- | --- | --- |
| No cap | 0.56% | 216 s | 1,370 s |
| Cap 900 s | 0.06% | 235 s | 928 s |
| Cap 600 s | 0.35% | 311 s | 790 s |
| Real | — | 212 s | 550 s |

The cap also shrinks the search radius (cap × speed ÷ detour), so it saves CPU too.

## 2. Skip work that can't succeed
Every 1-second round, each still-waiting rider searches for a car. If it failed last round and **no new idle car appeared nearby**, it will fail again, so skip it. Implementation: `state.Mem` keeps a generation counter and a log of idle-car additions. Each round, newly idle cars "wake up" failed riders in nearby cells. Everyone else stays asleep.

The first version did it the other way around (each rider scanned the addition log), and pprof showed that costing 75% of CPU. Inverting the loop made it cheap. **Profile before and after every optimization.**

## 3. The A/B test that caught a real bug
An optimization should change **speed, not answers**. So we added a `-no-skip` flag and ran the same binary both ways:
- 2-hour window: identical checksums. ✅
- Full day: **different checksums**. ❌

The bug: a rider searches its **top K = 8** nearest cars. If all 8 got claimed by other riders earlier in the same round, it fails, and we marked it "hopeless until a new car appears". But a 9th idle car, just beyond its top 8, was still there. The rider slept when it could have been served.

**Fix:** only memoize a failure when the search saw *every* car in range (fewer than K results). Then a regression test that reproduces exactly that situation (6 cars, 6 riders, K = 2), and the full-day checksums match:

| | Runtime | trips.csv sha1 |
| --- | --- | --- |
| skip off | 4m18s | b975ed60… |
| skip on | 2m37s | b975ed60… |

Lessons:
- **Short tests miss rare paths.** The 2-hour window simply never hit "all top-K claimed in one round".
- **Determinism is what made this findable.** Because the simulator is byte-reproducible, "different checksum" can only mean "different logic".
- **One earlier comparison was a false alarm, for a different reason.** Old vs. new *binary* differed because a refactor changed float rounding of the driver target (off by one car in 6 of 1,481 minutes). One car is enough to change the whole day's trajectory: chaotic systems amplify tiny differences. So compare the same binary with a flag, not two binaries.
- **The bug invalidated a result.** The first "cap 600" numbers came from the buggy binary, so they were re-run and replaced. Never keep a number produced by code you now know was wrong.

## 4. Calibrating supply per hour
Real driver supply isn't "75% of active cars are busy" every hour. We know busy vehicles per hour from the data; online drivers = busy ÷ utilization, so we fit **one utilization per hour** to make the simulated median wait match the real one.

It's a **fixed-point iteration**: run the day, compare sim vs. real wait per hour, nudge each hour's utilization, repeat. Version 1 oscillated (error 0.76 → 0.56 → 0.99) because the hours aren't independent: drivers carried over from 7 am affect 8 am. Version 2 damps the update (`ratio^0.3`) and caps each step at ±8%, the same tricks as a learning rate and gradient clipping.

Results: see `bench/calibration/` and PROGRESS.md.

## Interview questions this answers
- "Why can more dispatch aggressiveness make waits worse?"
- "How do you prove an optimization doesn't change behavior?"
- "Tell me about a bug your tests didn't catch."
- "How would you calibrate a simulator to reality?"
