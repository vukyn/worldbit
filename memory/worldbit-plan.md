---
name: worldbit-plan
description: "pet-platform repo `worldbit` — deterministic agent-based world sim (WorldBox-lite), Go+ebiten; P0-P2 shipped, P3 in progress, param sweep moved into P4"
metadata: 
  node_type: memory
  type: project
  modified: 2026-07-31T08:43:19.499Z
---

`worldbit` (`github.com/vukyn/worldbit`, public) — standalone repo created 2026-07-31. Deterministic agent-based world simulator, Go + ebiten, from gobuild `base` preset. Standalone binary like sgo/gobuild/speedtest — no DB/DI/domains/clean-arch/kuery.

**Full plan lives IN the repo:** `worldbit/docs/plan.md` (P0…P6, committed).

**Progress:** P0+P1 on main (`d9a1664`). P2 agents = PR #1 `feat/p2-agents`, verified green, **not merged yet**. P3 classifier in progress.

**Decision 2026-07-31 — parameter sweep moved from P7 up into P4.** Seeds 1-8 behave near-identically (peak 2163-2964 always around tick 760-822, settling 696-820, zero extinctions), so sweeping *seeds* yields ~1000 near-identical runs, mostly STABLE/TIMEOUT. Variety lives in *parameters*, not seeds. **Why:** the user's goal is "thử và sai liên tục nhiều simulation", which a flat seed sweep does not deliver. **How to apply:** P4's runner fans out over a parameter grid (e.g. `InitFoodPerCell × ReproEnergyCost`) × N seeds per cell, not just a seed range — same worker pool, little extra code. Never hand-tune the ratified defaults to force a varied outcome mix; the sweep answers that with data.

Product shape: **experiment harness with a viewer, not a game.** Batch-run N seeds headless → classify outcome (EXTINCT/STABLE/OSCILLATING/OVERRUN/DECLINING/TIMEOUT) → CSV → replay an interesting seed in the GUI **by re-simulating from the seed** (no snapshots on disk).

**Why:** user wants idle/observe simulation with trial-and-error over many seeds; bit-exact determinism (same seed → same output) was their stated hard requirement, so the whole architecture is subordinated to it.

**How to apply:** the non-obvious design constraints, all decided —
- `internal/sim` is stdlib-only and bans **maps of any kind** (Go randomizes `range` over maps), floats (FMA differs arm64 vs amd64), `time`, `math/rand`, goroutines, channels. Enforced by an AST lint test, not convention.
- Per-agent RNG substream `rng(seed, agentID, tick, purpose)` — a shared sequential stream is forbidden because removing one agent would shift everyone else's draws.
- Grid 128×128 torus, `TicksPerYear=120`, carrying capacity ≈820, pop >5000 = bug.
- Classifier arithmetic is exact-integer (128-bit compare via `math/bits.Mul64`), never float.
- `make golden` regenerating hash fixtures needs a commit message justifying the behavioural change — it's the path of least resistance that would hollow out the whole determinism guarantee.

Related: [[gobuild-preset-system]], [[sgo-onboarded]].
