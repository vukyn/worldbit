---
name: worldbit-p5-gui
description: worldbit P5 ebiten viewer built 2026-07-31 — the ebiten/headless gotchas, how to smoke-test a GUI when macOS blocks screenshots, and the food-banding artefact the picture revealed
metadata:
  type: project
---

worldbit P5 (the ebiten viewer) landed on branch `feat/p5-gui` (2026-07-31): `internal/sim/frame.go`
(`Frame` + `RenderInto`, the GUI's only read path) plus `World.Clone` for the scrub ring, and
`internal/gui/` (game/render/sparkline/hud/input/snapshot), every file `//go:build !nogui`.

**Three environment gotchas that cost real time — check these before debugging your own code:**

- **ebiten's `internal/ui` package `init()` builds the GLFW/NSWindow stack at process start.** It
  runs on *import*, so on the default build even `--headless` needs a display: with no window-server
  session it dies in `initialMonitorByOS` with a nil-monitor SIGSEGV. It happened here once, then
  stopped reproducing when the display came back. The `-tags nogui` build is therefore not merely a
  convenience for machines lacking GL headers (the plan's framing) — it is the only build that is
  safe to run on a server or in CI at all.
- **`ebiten.RunGame` cannot run under `go test`**: tests execute off the main goroutine, so window
  creation aborts with "NSWindow should only be instantiated on the main thread!". Any harness that
  drives the real loop must be a `package main`, not a test.
- **`screencapture` and `osascript` keystrokes are both blocked** in this sandbox (no Screen
  Recording / Accessibility grant). The technique that worked: a temporary `//go:build scratch`
  `RunCapture` in the gui package plus a scratch-tagged `scratchcmd/main.go`, wrapping the real
  `Game` so `Draw` renders into an offscreen `*ebiten.Image`, `ReadPixels` + `png.Encode` it, and
  input is driven by calling `apply(command{…})` directly at chosen frames. Real loop, real Draw,
  inspectable PNGs, no permissions. Delete both files before finishing.

**Design points worth not re-deriving:** the classifier thresholds are mapped once by
`runner.ClassifierParams` and passed into `gui.Options` from `main` — the viewer must never derive
its own, or the HUD and the CSV would eventually disagree. `stats.StopsRun` was lifted out of
`internal/runner` for the same reason: the viewer has to stop on the tick the harness stopped on or
every extinct run reports a spurious replay mismatch. Rewinding rebuilds the classifier by replaying
the truncated population history into a fresh `Recorder` rather than reversing the state machine.

**What the picture showed that the numbers never did:** food regrowth is *spatially coherent*. The
stride in `sim.regrow` (`for index := Tick % FoodRegrowTicks; …; index += stride`) means adjacent
cell indices regrow one tick apart, and index runs along x, so a regrowth wave sweeps the board as a
travelling horizontal front. Every frame shows food as long horizontal streaks. The stride comment
correctly says it avoids a synchronised global *pulse*, but it trades that for a moving harvest
front the population is implicitly chasing. No statistic in the harness can see this.

Also confirmed visually: at `BurnPerTick=5 FoodRegrowTicks=400` the sparkline shows a dozen regular
large-amplitude cycles — the OSCILLATING label there is genuine ecology, matching the measurement in
[[worldbit-classifier-measurement]] rather than the noise case in [[worldbit-p4-sweep-map]].

Related: [[worldbit-p1-determinism-harness]].
