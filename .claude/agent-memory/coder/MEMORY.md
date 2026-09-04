# Memory Index

- [worldbit P2 dynamics](worldbit-p2-dynamics.md) — steady state ~770; defaults now 1000/1000 STABLE, so a seed batch can no longer exercise the classifier — use a sweep
- [worldbit classifier measurement](worldbit-classifier-measurement.md) — scratch-tagged harness + overdispersion CV·√mean to pick thresholds; genuine oscillation = BurnPerTick 5 × regrow 300-600
- [worldbit regrowth front](worldbit-regrowth-front.md) — regrowth swept the board as a travelling front; invisible to every statistic, and it was producing most of the "seed variance"
- [worldbit GUI frame capture](worldbit-gui-frame-capture.md) — live screencapture is blocked (and grabs the user's desktop); render frames offline through the real paint path instead

## Moved in from the platform-level store (2026-09-05)

⚠️ These were written while the session's working directory was the platform
root, so they landed in `<root>/.claude/agent-memory/` where this repo's own
agent could not see them — same knowledge, same agent, different cwd. They live
here now, and new ones belong here.

- [worldbit classifier gates](project_worldbit_classifier_gates.md) — 3 OSCILLATING gates; MinOscillatingWindows opt-out is 1 not 0, total-not-consecutive, per-knob test isolation
- [worldbit P1](project_worldbit_p1.md) — new repo 2026-07-31, P0+P1 determinism harness only (no agents); plan in docs/plan.md; stale gobuild + seed-independent goldens gotchas
- [worldbit P4 sweep map](project_worldbit_p4_sweep_map.md) — runner+sweep+burn-in 2026-07-31; FoodRegrowTicks dominates, defaults quiet, EXTINCT needs BurnPerTick; counts now STALE
- [worldbit P5 GUI](project_worldbit_p5_gui.md) — ebiten viewer 2026-07-31; ebiten init needs a display even headless, RunGame can't run under go test, scratch-main PNG capture, food banding artefact
