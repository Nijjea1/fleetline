# bench/

One directory per experiment (E1–E8). Each has a single entry script, a config, and a `results/` folder with summarized outputs that are committed. Raw dumps go to `raw/` (gitignored).

Rules (from the scope doc):
1. Same replayed day, same seed, one variable changed.
2. Run each config several times; report the spread.
3. Keep the losing results.
