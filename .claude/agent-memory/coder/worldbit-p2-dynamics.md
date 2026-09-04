---
name: worldbit-p2-dynamics
description: worldbit default-config dynamics — steady state ~770, and since the regrowth fix a 1000-seed default batch is 1000/1000 STABLE, so seed batches can no longer exercise the classifier
metadata:
  type: project
---

With the ratified `DefaultConfig()` every seed has the same shape: an initial-pantry boom peaking
around tick 760-820, then a settle to a steady state for the rest of a 12 000-tick run. No seed
goes extinct; none approaches the OVERRUN threshold of 6 553.

**Current numbers (after the 2026-08-01 regrowth-permutation fix):** peaks 2276-2974, finals
739-787, and a 1000-seed batch is **1000 STABLE — every single seed**. Before the fix: peaks
2163-2964, finals 696-820, and 966 STABLE / 33 TIMEOUT / 1 DECLINING (seed 5 was the DECLINING
one). See [[worldbit-regrowth-front]] for why the spread collapsed.

`docs/plan.md` predicted a carrying capacity of ~820 (16384 cells ÷ 200 ticks × 10 energy ÷ 1 burn)
but "a real steady state expected around 400-600". The observed steady state lands on the
arithmetic capacity instead, i.e. **above the plan's expectation but exactly on its own formula**.

**Why:** the parameters are the user's, ratified in the plan. A mismatch between prediction and
measurement is information to report, not a bug to paper over by quietly retuning
`InitFoodPerCell` / `ReproEnergyCost`.

**How to apply:** a default-config seed batch is now a **useless** classifier exercise — it can
only ever produce STABLE. Use a `--sweep` over `FoodRegrowTicks` × `BurnPerTick`, never `--runs N`
at the defaults. A batch that comes out all-EXTINCT or all-OVERRUN means something regressed in the
sim, not that the ecology is delicate. If a parameter change is ever wanted to widen the outcome
mix, raise it with the user first. Relates to [[worldbit-classifier-measurement]].
