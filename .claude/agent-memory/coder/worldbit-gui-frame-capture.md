---
name: worldbit-gui-frame-capture
description: How to see worldbit's viewer output — live screencapture is permission-blocked on this Mac, so render frames offline through the real paint path instead
metadata:
  type: project
---

Live window capture of the ebiten viewer **does not work on this machine**. `osascript` cannot
send keystrokes or focus the window ("not allowed to send keystrokes, 1002") and `screencapture`
mostly returns "could not create image from display". When it does fire it grabs the **whole
desktop**, i.e. the user's private screen — delete such files immediately rather than inspecting
them.

The working route is an offline render, and it is strictly better anyway because it is
deterministic and can target an exact tick:

- Add a throwaway `//go:build scratch && !nogui` test in `internal/gui`. It can call the real
  `foodRamp` and `paintFrame` (both package-level, non-test) plus `sim.RenderInto` — the exact
  three calls `Draw` makes — then upscale into an `image.RGBA` and `png.Encode` it.
- Run a specific tick with `sim.NewWorld(seed, cfg)` + `sim.Run(world, tick)`.
- For a before/after pair, `git worktree add <scratch>/basetree <ref>`, copy the same scratch file
  in, run both. Remove the worktree afterwards (`git worktree remove --force`, then `prune`).
- Delete the scratch test before finishing; it is not a deliverable.

**Why:** the travelling-front regrowth artefact was invisible to every statistic and could only be
found and verified visually — see [[worldbit-regrowth-front]]. Cross-check that the offline render
equals the live one by matching the HUD's population against the frame's `Pop` at the same tick.

**How to apply:** any worldbit question of the form "does it *look* right" goes through this
harness, not through screencapture. The determinism lint only inspects non-test files in
`internal/sim` and `internal/stats`, so a `_test.go` scratch file may import `os`, `image/png`
and anything else freely.
