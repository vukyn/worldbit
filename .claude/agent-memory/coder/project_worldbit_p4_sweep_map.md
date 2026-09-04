---
name: worldbit-p4-sweep-map
description: worldbit P4 (runner+sweep+burn-in) built 2026-07-31; the measured parameter map — FoodRegrowTicks dominates, defaults sit in a quiet region, EXTINCT needs BurnPerTick not food
metadata:
  type: project
---

worldbit P4 landed on branch `feat/p4-runner-sweep` (2026-07-31): `internal/runner` (worker
pool, CSV/JSON, per-cell aggregate), `--sweep` parameter grids in `internal/config/sweep.go`,
and classifier burn-in (`BurnInWindows`, default 1).

**Why the sweep matters:** seed variation alone at the ratified defaults produces near-identical
runs — a 1000-seed batch is 966 STABLE / 33 TIMEOUT / 1 DECLINING, with EXTINCT, OVERRUN and
OSCILLATING unreachable. The variety lives in the parameters, so the sweep is the harness's
actual payload.

**The measured map** (45 cells, `InitFoodPerCell` × `ReproEnergyCost` × `FoodRegrowTicks`,
20 seeds each). `FoodRegrowTicks` dominates because it sets carrying capacity
(`cells / FoodRegrowTicks × EnergyPerFood / BurnPerTick`); the other two only move the height
of the opening boom:

| FoodRegrowTicks | capacity | outcome |
|---|---|---|
| 25 | ~6550 | OVERRUN, every seed |
| 100 | ~1640 | STABLE, every seed |
| 400 | ~410 | STABLE/TIMEOUT mix, a few DECLINING |
| 1600 | ~102 | TIMEOUT, a few DECLINING |
| 6400 | ~26 | OSCILLATING, every seed |

Defaults are `FoodRegrowTicks=200`, between the all-STABLE and mixed bands.

**How to apply — three findings that will save re-deriving them:**

- **EXTINCT is not reachable by starving the board.** Even `FoodRegrowTicks=12000` (capacity
  ~14) leaves a surviving remnant. It needs a *metabolic* squeeze: `BurnPerTick=5` extinguishes
  every seed at regrow ≥ 1600; `BurnPerTick=2–3` gives a 50-90 % extinction rate.
- **OSCILLATING in this parameter space is never a large-amplitude boom-bust cycle.** It is a
  starving remnant of 10-40 agents whose small-number noise trivially exceeds the scale-free
  CV>0.25 predicate. The near-overrun band (regrow 30-90), where real overshoot cycles would
  live, produces only STABLE and OVERRUN. Do not read OSCILLATING as "cycling ecology".
- **Per-run cost is 350 ms solo but ~553 ms under a saturated 8-worker pool** — the sim is
  memory-bound over the 16 384-cell grid, so workers contend rather than scale linearly. The
  runner's wall-time estimate uses the saturated figure deliberately.

The ratified defaults were NOT retuned; the map is the evidence the user asked for so they can
decide. `docs/plan.md` was reconciled with the code in the same change (burn-in, `--sweep` folded
into P4, corrected perf figures) and now carries an "Experimental findings (P4)" section holding
the map above — **read that before P5/P6 rather than re-deriving it**.

⚠️ **CLOSED, and two of the findings above were wrong.** The open OSCILLATING design question was
ratified and fixed twice over (`MinOscillatingPopulation`, then `MinOscillatingWindows`), and
genuine large-amplitude cycles DO exist — at high burn and moderate regrowth, a band the P4 probe
never covered. See [[worldbit-classifier-gates]]. ⚠️ **Every outcome count on this page also
predates the 2026-08-01 regrowth-permutation fix and is not comparable to anything measured after
it** — re-measure, do not reuse.

Related: [[worldbit-p1-determinism-harness]], [[worldbit-classifier-gates]], and worldbit's own
repo-local coder memory `worldbit/.claude/agent-memory/coder/worldbit-p2-dynamics.md`.
