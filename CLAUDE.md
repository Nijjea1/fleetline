# Fleetline: rules for coding agents

Real-time NYC ride-dispatch resume project: Go core (Kafka/Redpanda + Redis), a deterministic offline simulator, matching solvers (`matcher/`), ML ETA model (`ml/`), and a Next.js/deck.gl dashboard (`web/`).

**On the GPU (RTX 5060) machine: follow `docs/GPU_MACHINE.md` task by task.**

## Where things are
- What's left: `docs/ROADMAP.md`. History: `docs/PROGRESS.md`. Why things are the way they are: `docs/DECISIONS.md` (D-001…). Resume claims: `docs/RESUME_CONTEXT.md`.
- Experiments live in `bench/` (E1 load, E2/E3 matching, E4 solvers, offers) and `ml/results/` (E5/E6). Each has a script that writes a result file.
- `data/` is gitignored (rebuild instructions in the README "Run it" section).

## Rules
- **Every number in README / RESUME_CONTEXT comes from a committed result file produced by a script.** Never estimate or round up a claim. Record losing results too.
- **Fair experiments:** same day, same seed, one variable changed. Multiple seeds, report the spread. Tune on Sep 16 only; Sep 23 is held out (run once with final settings).
- **Timing benchmarks:** plugged in, high-performance power mode, otherwise idle machine. Note the hardware in the result file.
- **Git:** small, real commits with clear messages, pushed after each step. Never set GIT_AUTHOR_DATE / GIT_COMMITTER_DATE, never rewrite history. Never commit data, secrets, `.env*` or `.vercel/`. End commit messages with:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`
- **Teach as you go:** the owner is learning. Explain what you're doing and why in plain language, and add a short learning note in `docs/learning/` for each new concept.
- The repo is private: don't make it public or change settings without asking.

## Checks before pushing
`make test` (Go + Python). Matcher alone: `cd matcher && uv run pytest -q && uv run ruff check`. Web: `cd web && npx tsc --noEmit && npm run build` (if the Turbopack build panics, `rm -rf .next`).
