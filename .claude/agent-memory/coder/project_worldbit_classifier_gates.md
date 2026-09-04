---
name: worldbit-classifier-gates
description: worldbit's OSCILLATING label now has three independent gates (burn-in, population floor, sustained windows); the traps are the 1-not-0 opt-out, the consecutive/floor interaction, and per-knob test isolation
metadata:
  type: project
---

worldbit's `stats.Classifier` gates the OSCILLATING verdict with **three** knobs added at three
different times, each ratified separately and each with a documented opt-out:

| knob | default | opt-out | what it stops |
|---|---|---|---|
| `BurnInWindows` | 1 | **0** | the startup boom deciding every run |
| `MinOscillatingPopulation` | 64 | **0** | a starving remnant clearing a scale-free CV |
| `MinOscillatingWindows` | 2 | **1** | one transient excursion latching the verdict |

**Why:** each was measured on the same 1200-run `FoodRegrowTicks` × `BurnPerTick` grid and ratified
by the user after being reported as a defect. The third landed 2026-08-03 on branch
`fix/sustained-oscillation`; it took OSCILLATING 108 → 78, all 30 removed runs falling to
TIMEOUT/DECLINING via the existing fallback.

**How to apply — four things that will otherwise be re-derived or got wrong:**

- **`MinOscillatingWindows`'s opt-out is 1, not 0**, breaking the pattern the other two set. Zero is
  rejected by both `Validate()` and `NewClassifier` because "at least zero windows fired" is true
  for every run. Do not "fix" the inconsistency.
- **It counts TOTAL firing windows, not a consecutive streak — deliberately, against the obvious
  symmetry with `StableWindows=3`.** A consecutive rule compounds with the population floor: a
  cycle whose mean sits near the floor fails the *floor* in its trough windows, so every trough
  resets the streak. Measured on the cell `FoodRegrowTicks=600`/`BurnPerTick=5` (a real cycle at
  mean population 52–56 against a floor of 64), total-2 keeps 18/20 runs and consecutive-2 keeps
  4/20. `StableWindows` was deliberately NOT lifted into `sim.Config` alongside it.
- **Every test of one classifier knob must hold the OTHER knobs at their opt-out.** Adding the
  third gate broke four existing tests, all because burn-in and population-floor tests drive series
  with a *single* firing window — they would otherwise have passed for the new gate's reason rather
  than their own. `runner/variation_floor_test.go` has a `floorTestConfig()` helper for exactly
  this.
- **The independent label used to justify each threshold** counts large excursions off a smoothed
  100-tick-block population series (Schmitt trigger at ±25 % of the post-burn-in mean). It knows
  nothing about the 1200-tick window grid, so it is not circular with a window-counting rule, and
  it separates the ecology cleanly — the 108 OSCILLATING runs split bimodally into 28 runs with
  0–8 swings and 80 with 10–21. Reuse it rather than inventing a new one.

Related: [[worldbit-p4-sweep-map]] (the parameter map, with its pre-regrowth-fix caveat),
[[worldbit-p5-gui]], [[worldbit-p1-determinism-harness]].
