---
name: worldbit-regrowth-front
description: worldbit's regrowth stride swept the board as a travelling front — invisible to every statistic, caught only by watching the GUI, and it was the dominant source of seed-to-seed variance
metadata:
  type: project
---

`sim.regrow` used to stride over raw cell indices. Index runs along x first, so horizontally
adjacent cells regrew one tick apart and recovery crossed the grid as a **travelling horizontal
front**: food in long streaks, agents banded up behind them. Fixed 2026-08-01 by striding over
positions in `regrowOrder`, a fixed (NOT seed-derived) permutation built by Fisher-Yates.

Three things worth carrying forward:

- **No statistic in the harness could see it.** Population, peak, final, outcome, overdispersion —
  all blind to a purely spatial correlation. It was found by looking at the viewer. When a change
  is "spatial" or "visual", the CSV is not evidence; render a frame.
- **The front was producing most of the seed variance.** Removing it cut final-population SD from
  44.9 to 19.2 over 1000 seeds (CV 5.83 % → 2.50 %). Any future "this project lacks seed variance"
  hypothesis should be checked against the possibility that the variance being missed was itself an
  artefact. A seed-derived permutation was measured as the fix for exactly that and added **zero**
  variance (F-ratio 1.0028, n=1000) — hence fixed, which is also the more testable choice.
- **It was doing ecological work on STATIONARITY, not on capacity.** Carrying capacity is
  unchanged (mean final pop moves <2 % anywhere on the `FoodRegrowTicks` axis, OVERRUN boundary
  identical) but STABLE went 310→507 on the 1200-run grid, almost all of it TIMEOUT converting:
  populations sat at capacity all along and simply never held still. EXTINCT rose 137→189 at the
  harsh corner — scattered single cells are harder to forage than streaks, a real cost.

**Why it matters:** every sweep number recorded before 2026-08-01 is non-comparable, and
`docs/plan.md` "Experimental findings" carries a banner saying so. Don't put a pre-fix and a
post-fix figure in the same table.

**How to apply:** for any future environment/spatial change, verify by rendering frames through
the real paint path, and re-measure the grid rather than reusing recorded counts. Relates to
[[worldbit-p2-dynamics]], [[worldbit-classifier-measurement]], [[worldbit-gui-frame-capture]].
