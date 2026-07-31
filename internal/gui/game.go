//go:build !nogui

// Package gui is the viewer: an ebiten window that replays a seed by
// RE-SIMULATING it rather than by loading stored state.
//
// Three properties hold everywhere in this package.
//
// It is read-only over the simulation. Every pixel comes from a sim.Frame,
// which the world fills and the renderer only reads; nothing here writes a
// World field. The one world this package steps is the one it created itself.
//
// It is fixed-tick. Step takes no dt and never consults a clock. The speed
// control decides HOW MANY ticks a displayed frame advances, and the frame
// budget in advance decides how many fit in a "max" frame — wall-clock
// therefore decides how far the simulation has got when you look at it, never
// what state N contains. That is the one legitimate appearance of wall-clock
// anywhere near the simulation, and it is confined to advance below.
//
// Every file carries //go:build !nogui, so `go build -tags nogui ./...`
// produces a binary with no graphics stack linked in at all. The batch harness
// is the primary deliverable and has to stay buildable on a machine with no GL
// or X11 headers.
package gui

import (
	"fmt"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/vukyn/worldbit/internal/sim"
	"github.com/vukyn/worldbit/internal/stats"
)

// frameBudget is how long a "max speed" frame may spend stepping the world.
//
// Eight milliseconds of a sixteen-millisecond frame leaves room for the render
// and keeps the window responsive to input; a viewer that stops answering the
// pause key while it races is worse than a slower one. This is the wall-clock
// mentioned in the package comment: it bounds how far the simulation runs
// before it is drawn, and has no influence whatsoever on what any given tick
// computes.
const frameBudget = 8 * time.Millisecond

// budgetCheckStride is how many ticks pass between clock reads inside a max
// speed frame. A tick costs tens of microseconds and a clock read tens of
// nanoseconds, so reading every tick would be affordable — but batching keeps
// the syscall out of the hot loop and the overshoot is at most a few hundred
// microseconds.
const budgetCheckStride = 16

// speedMax is the ticksPerFrame sentinel for "as many ticks as the frame
// budget allows".
const speedMax = -1

// Options is everything the viewer needs to reconstruct a run.
//
// Classifier arrives already mapped from the simulation config rather than
// being derived here, so the viewer's HUD label and the batch harness's CSV
// column are produced by one set of thresholds. Two mappings would eventually
// disagree, and the first symptom would be a run the CSV calls STABLE and the
// window calls TIMEOUT.
type Options struct {
	Config     sim.Config
	Seed       uint64
	Scale      int
	Classifier stats.ClassifierConfig

	// ExpectHash is a recorded state_hash to check the replay against, and
	// HasExpect says whether one was supplied. See replayVerdict.
	ExpectHash uint64
	HasExpect  bool

	// Version is reported in the window title, so a screenshot names the
	// binary that produced it.
	Version string
}

// Run opens the viewer and blocks until the window closes.
//
// It must be called from the goroutine running main: the platform windowing
// APIs ebiten drives are main-thread only, so RunGame is deliberately NOT
// wrapped in a goroutine here or anywhere else.
func Run(options Options) error {
	game := newGame(options)

	ebiten.SetWindowSize(game.width, game.height)
	ebiten.SetWindowTitle(fmt.Sprintf("worldbit %s — seed %d", options.Version, options.Seed))
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	return ebiten.RunGame(game)
}

// game is the ebiten.Game implementation and the whole of the viewer's state.
type game struct {
	options Options

	// The one world this package owns. It is created here, stepped here, and
	// never handed out.
	world    *sim.World
	recorder *stats.Recorder
	frame    *sim.Frame
	ring     *snapshotRing
	seed     uint64

	input    inputSource
	commands []command

	ticksPerFrame int
	paused        bool

	entryActive bool
	entryDigits []byte

	canvas *ebiten.Image
	pixels []byte
	ramp   []rgba
	text   *textLayer

	gridWidth  int
	gridHeight int
	width      int
	height     int
}

func newGame(options Options) *game {
	if options.Scale < 1 {
		options.Scale = 1
	}

	g := &game{
		options:       options,
		frame:         &sim.Frame{},
		input:         keyboard{},
		ticksPerFrame: 1,
		gridWidth:     int(options.Config.Width) * options.Scale,
		gridHeight:    int(options.Config.Height) * options.Scale,
		ramp:          foodRamp(options.Config.FoodMax),
	}

	// The HUD needs a legible minimum width even when a small scale would give
	// the grid less; the grid is left-aligned in whatever width results.
	g.width = g.gridWidth
	if g.width < minWindowWidth {
		g.width = minWindowWidth
	}
	g.height = g.gridHeight + hudHeight

	g.reset(options.Seed)
	return g
}

// reset restarts the run from a seed. Everything derived from the trajectory —
// the classifier, the population history, the scrub snapshots — is discarded,
// because none of it describes the new run.
func (g *game) reset(seed uint64) {
	g.seed = seed
	g.world = sim.NewWorld(seed, g.options.Config)
	g.recorder = stats.NewRecorder(g.options.Classifier, int(g.options.Config.MaxTick))
	g.ring = newSnapshotRing(g.options.Config.MaxTick)
}

// Layout fixes the logical resolution. The window may be resized; the contents
// scale rather than reflow, which keeps one pixel of the grid an exact multiple
// of one pixel of the canvas at the default size.
func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return g.width, g.height
}

func (g *game) Update() error {
	g.commands = appendCommands(g.commands[:0], g.input, g.entryActive)
	for _, next := range g.commands {
		if err := g.apply(next); err != nil {
			return err
		}
	}

	if g.paused || g.entryActive {
		return nil
	}
	g.advance()
	return nil
}

// apply performs one command. It is deliberately free of every ebiten call so
// that the whole control surface — speeds, restart, seed entry, scrubbing — is
// testable without a window.
func (g *game) apply(next command) error {
	switch next.kind {
	case commandQuit:
		return ebiten.Termination

	case commandTogglePause:
		g.paused = !g.paused

	case commandSetSpeed:
		g.ticksPerFrame = next.value
		g.paused = false

	case commandRestart:
		g.reset(g.seed)

	case commandBeginSeedEntry:
		g.entryActive = true
		g.entryDigits = g.entryDigits[:0]

	case commandCancelSeedEntry:
		g.entryActive = false

	case commandSeedDigit:
		if len(g.entryDigits) < maxSeedDigits {
			g.entryDigits = append(g.entryDigits, byte('0'+next.value))
		}

	case commandSeedBackspace:
		if length := len(g.entryDigits); length > 0 {
			g.entryDigits = g.entryDigits[:length-1]
		}

	case commandCommitSeedEntry:
		g.entryActive = false
		// An unparseable or overlong entry leaves the run alone rather than
		// silently restarting on some truncated seed.
		if seed, ok := parseSeed(g.entryDigits); ok {
			g.reset(seed)
		}

	case commandScrub:
		g.scrub(next.value)
	}

	return nil
}

// advance runs the ticks this displayed frame is worth.
func (g *game) advance() {
	if g.complete() {
		return
	}

	if g.ticksPerFrame != speedMax {
		for i := 0; i < g.ticksPerFrame && !g.complete(); i++ {
			g.step()
		}
		return
	}

	deadline := time.Now().Add(frameBudget)
	for !g.complete() {
		for i := 0; i < budgetCheckStride && !g.complete(); i++ {
			g.step()
		}
		if !time.Now().Before(deadline) {
			return
		}
	}
}

// step advances exactly one tick and records it.
func (g *game) step() {
	sim.Step(g.world)
	g.recorder.Observe(int(g.world.Population()))
	g.ring.capture(g.world)

	// Resolving as soon as the run is over means the HUD shows the same
	// verdict the batch harness would have written, on the tick it was
	// reached, rather than sitting on RUNNING forever.
	if g.complete() {
		g.recorder.Finish()
	}
}

// complete reports whether there is nothing left to simulate.
//
// The stopping rule is stats.StopsRun, the same predicate the batch harness
// uses, because the replay check compares this world's hash against a
// state_hash recorded at whatever tick the harness stopped on. A viewer that
// ran an extinct world on to MaxTick would hash a different moment and report
// a mismatch on a perfectly reproducible run.
func (g *game) complete() bool {
	return g.world.Tick >= g.options.Config.MaxTick || stats.StopsRun(g.recorder.Outcome())
}

// scrub moves the run by a signed number of ticks and pauses, because scrubbing
// is something you do in order to look at a moment rather than to pass through
// it.
func (g *game) scrub(delta int) {
	g.paused = true

	target := int64(g.world.Tick) + int64(delta)
	if target < 0 {
		target = 0
	}
	if limit := int64(g.options.Config.MaxTick); target > limit {
		target = limit
	}

	switch {
	case target < int64(g.world.Tick):
		g.rewind(int32(target))
	case target > int64(g.world.Tick):
		for int64(g.world.Tick) < target && !g.complete() {
			g.step()
		}
	}
}

// rewind moves the run backwards to an earlier tick.
//
// Nothing is un-simulated: the world is rebuilt from the nearest earlier
// snapshot (or from the seed, if the target is before the first one) and
// re-stepped forward, which the determinism contract guarantees reproduces the
// exact state that was there before.
//
// The classifier is rebuilt rather than reversed. It is a pure function of the
// population series and the series is already in hand, so replaying the
// truncated history into a fresh recorder is both exact and effectively free —
// twelve thousand integer observations cost far less than one simulated tick.
func (g *game) rewind(target int32) {
	history := g.recorder.History()
	if int(target) > len(history) {
		return
	}

	world := g.ring.nearest(target)
	if world == nil {
		world = sim.NewWorld(g.seed, g.options.Config)
	} else {
		world = world.Clone()
	}
	for world.Tick < target {
		sim.Step(world)
	}

	recorder := stats.NewRecorder(g.options.Classifier, int(g.options.Config.MaxTick))
	for _, population := range history[:target] {
		recorder.Observe(population)
	}

	g.world = world
	g.recorder = recorder
}

// verdict is the result of the replay check.
type verdict uint8

const (
	// verdictNone means no expected hash was supplied.
	verdictNone verdict = iota
	// verdictPending means the run has not reached its stopping point yet, so
	// there is nothing to compare.
	verdictPending
	verdictMatch
	verdictMismatch
)

// replayVerdict compares the finished run against the recorded state_hash.
//
// This is the return on the whole determinism effort: replaying a seed from a
// CSV row and hashing the result turns every viewing session into a live
// regression test against a real recorded run, catching drift that the unit
// tests would only notice at the next golden regeneration.
func (g *game) replayVerdict(hash uint64) verdict {
	switch {
	case !g.options.HasExpect:
		return verdictNone
	case !g.complete():
		return verdictPending
	case hash == g.options.ExpectHash:
		return verdictMatch
	default:
		return verdictMismatch
	}
}

// speedLabel names the current speed for the HUD.
func (g *game) speedLabel() string {
	switch {
	case g.paused:
		return "paused"
	case g.ticksPerFrame == speedMax:
		return "max"
	default:
		return fmt.Sprintf("%dx", g.ticksPerFrame)
	}
}
