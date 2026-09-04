---
name: worldbit-classifier-measurement
description: How to answer worldbit classifier-threshold questions with evidence — scratch-tagged measurement harness + overdispersion metric; and where genuine oscillation actually lives
metadata:
  type: project
---

Classifier-threshold questions in worldbit ("is this label lying?", "what should this floor be?")
are answered by **measuring**, not by reasoning from the ecology. The technique that worked:

- Add a **temporary** `//go:build scratch` test in `internal/runner` (it is the only package that
  imports both `sim` and `stats`). Mirror `runOne`'s loop, run a shadow `stats.Window` alongside the
  real classifier, and dump per-window `n, S1, S2, min, max, hv, declining` to CSV. Analyse in
  Python. Delete the harness before finishing — it is not a deliverable.
- The discriminator that actually separates signal from noise is **overdispersion = CV·√mean**
  (1.0 = pure demographic noise). Raw CV is useless on its own because it is scale-free, and
  amplitude (`max−min`) does **not** discriminate at all — measured, TP and FP distributions
  overlap with FP slightly higher.
- `sweep*.json` and `runs*.csv` are gitignored, so past experiment grids are **not** recoverable
  from the repo. Reconstruct the grid and validate the reconstruction against the numbers recorded
  in `docs/plan.md` before trusting it.

**Where genuine oscillation lives:** `BurnPerTick=5` with `FoodRegrowTicks` **300–600** (20/20 every
cell), extending weakly down to 200; plus a pocket at 600/`burn=4`. P4's "large-amplitude sustained
oscillation appears not to exist" was a **coverage gap** — it only probed the `FoodRegrowTicks` axis
at `BurnPerTick=1`. Do not repeat that conclusion. (Region re-measured after the 2026-08-01
regrowth-permutation fix, which moved its edges but not its core — it used to read 300–800, and 800
has since tipped towards EXTINCT. See [[worldbit-regrowth-front]]; pre-fix grid counts are not
comparable to post-fix ones.)

**Why:** P4 asserted OSCILLATING was pure mislabelling and that no real cycles existed. Both halves
needed checking; the first was right, the second was wrong, and only measurement showed which.
A threshold picked "by feel" would have suppressed a real ecological regime.

**How to apply:** before changing any classifier threshold, measure the distribution the threshold
cuts and score candidate values against an independent ground-truth label (precision/recall), then
report the plateau — thresholds here have broad flat optima, so quote the range, not just the peak.
Relates to [[worldbit-p2-dynamics]].
