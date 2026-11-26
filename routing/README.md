# routing/: E7, Amazon Last Mile stop sequencing

**Question:** how close does an optimizer get to the order real Amazon drivers deliver in? Data: the [Amazon Last Mile Routing Research Challenge](https://registry.opendata.aws/amazon-last-mile-challenges/) (2021, CC BY-NC 4.0): 9,184 real routes from 5 metro areas, each with stops, zones, a stop-to-stop travel-time matrix and the driver's actual sequence.

```bash
# raw data (~3 GB) into data/raw/almrrc/{train,eval}/ : see data.py for the file list
uv run fleetline-routing prepare train && uv run fleetline-routing prepare eval
uv run fleetline-routing tune      # zone penalties, 200 training routes
uv run fleetline-routing eval      # 3,052 held-out routes, once
```

- **Score:** the challenge's official scorer (downloaded at run time, unmodified). Lower is better; 0 = the driver's exact order. Note: it also scores an exactly reversed order 0.
- **Methods:** `shortest` (fastest tour, OR-Tools guided local search) and `zones_*` (fastest tour after a penalty on edges that leave a delivery zone; flat, or graded by zone-id distance).
- **Protocol:** penalties tuned on training routes only; the evaluation set is run once.

**Result (held out):** shortest 0.0877 → flat 3,000 s zone penalty **0.0547 (−37.6%)**, better on 2,639 of 3,052 routes, tours 6% shorter than the drivers'. Graded penalties lost in tuning. Files: `bench/e7/results/`; write-up: `docs/learning/11-last-mile-routing.md`.
