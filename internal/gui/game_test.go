//go:build !nogui

package gui

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/vukyn/worldbit/internal/sim"
	"github.com/vukyn/worldbit/internal/stats"
)

// fakeKeyboard is a scripted input source. It lets every control the viewer has
// be exercised without a window, a GL context or a running game loop, which is
// otherwise the part of a GUI that never gets tested at all.
type fakeKeyboard struct {
	just map[ebiten.Key]bool
	held map[ebiten.Key]bool
}

func newFakeKeyboard() *fakeKeyboard {
	return &fakeKeyboard{just: map[ebiten.Key]bool{}, held: map[ebiten.Key]bool{}}
}

func (k *fakeKeyboard) justPressed(key ebiten.Key) bool { return k.just[key] }
func (k *fakeKeyboard) pressed(key ebiten.Key) bool     { return k.held[key] }

// press schedules one key for the next frame only, the way a real just-pressed
// edge behaves.
func (k *fakeKeyboard) press(keys ...ebiten.Key) {
	clear(k.just)
	for _, key := range keys {
		k.just[key] = true
	}
}

func (k *fakeKeyboard) release() { clear(k.just) }

// testOptions is a small, fast world: a 32x32 grid settles and runs out in a
// fraction of the time the default 128x128 board takes, and none of the control
// logic under test cares about the size.
func testOptions() Options {
	config := sim.DefaultConfig()
	config.Width = 32
	config.Height = 32
	config.InitAgents = 40
	config.MaxTick = 3000

	classifier := stats.DefaultClassifierConfig()
	classifier.TicksPerYear = int(config.TicksPerYear)
	classifier.OverrunPopulation = stats.OverrunPopulationFor(int(config.Width) * int(config.Height))
	classifier.BurnInWindows = int(config.BurnInWindows)
	classifier.MinOscillatingPopulation = int(config.MinOscillatingPopulation)

	return Options{Config: config, Seed: 1, Scale: 2, Classifier: classifier, Version: "test"}
}

func newTestGame(t *testing.T) (*game, *fakeKeyboard) {
	t.Helper()

	keyboard := newFakeKeyboard()
	g := newGame(testOptions())
	g.input = keyboard
	return g, keyboard
}

// update drives one frame, the way ebiten would.
func update(t *testing.T, g *game) error {
	t.Helper()
	return g.Update()
}

// TestSpeedControlAdvancesTheRightNumberOfTicks pins the fixed-tick contract
// from the viewer's side: the speed setting decides how many ticks a displayed
// frame is worth, and nothing else does.
func TestSpeedControlAdvancesTheRightNumberOfTicks(t *testing.T) {
	g, keyboard := newTestGame(t)

	keyboard.press(ebiten.KeyDigit1)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	if g.world.Tick != 1 {
		t.Fatalf("one frame at 1x advanced to tick %d, want 1", g.world.Tick)
	}

	keyboard.press(ebiten.KeyDigit2)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	if g.world.Tick != 11 {
		t.Fatalf("one frame at 10x advanced to tick %d, want 11", g.world.Tick)
	}

	keyboard.press(ebiten.KeyDigit3)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	if g.world.Tick != 111 {
		t.Fatalf("one frame at 100x advanced to tick %d, want 111", g.world.Tick)
	}
}

// TestPauseStopsTime is the control most likely to be broken by a refactor of
// Update, and the one whose failure is most obvious to a user.
func TestPauseStopsTime(t *testing.T) {
	g, keyboard := newTestGame(t)

	keyboard.press(ebiten.KeySpace)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !g.paused {
		t.Fatal("space did not pause")
	}

	keyboard.release()
	for frame := 0; frame < 10; frame++ {
		if err := update(t, g); err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	if g.world.Tick != 0 {
		t.Fatalf("the world advanced to tick %d while paused", g.world.Tick)
	}

	keyboard.press(ebiten.KeySpace)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	if g.paused {
		t.Fatal("space did not unpause")
	}
}

// TestMaxSpeedStopsAtTheTickLimit checks the one place wall-clock touches the
// simulation. The budget may decide how far a frame gets; it may not carry the
// run past its own limit.
func TestMaxSpeedStopsAtTheTickLimit(t *testing.T) {
	g, keyboard := newTestGame(t)

	keyboard.press(ebiten.KeyDigit4)
	for frame := 0; frame < 2000 && !g.complete(); frame++ {
		if err := update(t, g); err != nil {
			t.Fatalf("update: %v", err)
		}
		keyboard.release()
	}

	if !g.complete() {
		t.Fatalf("max speed did not finish the run; stopped at tick %d", g.world.Tick)
	}
	if g.world.Tick > g.options.Config.MaxTick {
		t.Fatalf("the run overshot its limit: tick %d > MaxTick %d",
			g.world.Tick, g.options.Config.MaxTick)
	}
	if outcome := g.recorder.Outcome(); outcome == stats.OutcomeRunning {
		t.Fatal("a completed run is still reporting RUNNING")
	}
}

// TestRestartReturnsToTickZero also covers the state that must NOT survive a
// restart: a history carried over from the previous run would draw a sparkline
// belonging to a world that no longer exists.
func TestRestartReturnsToTickZero(t *testing.T) {
	g, keyboard := newTestGame(t)

	keyboard.press(ebiten.KeyDigit3)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Paused first, so the frame that restarts does not also advance the new
	// run and blur what is being asserted.
	keyboard.press(ebiten.KeySpace)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	keyboard.press(ebiten.KeyR)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}

	if g.world.Tick != 0 {
		t.Errorf("restart left the world at tick %d", g.world.Tick)
	}
	if history := g.recorder.History(); len(history) != 0 {
		t.Errorf("restart left %d ticks of history behind", len(history))
	}
	if g.seed != 1 {
		t.Errorf("restart changed the seed to %d", g.seed)
	}
}

// TestSeedEntryStartsANewRun covers the mode, not just the parse: while entry
// is active the digits must build a seed rather than change the speed, and the
// world must not advance underneath the typing.
func TestSeedEntryStartsANewRun(t *testing.T) {
	g, keyboard := newTestGame(t)

	keyboard.press(ebiten.KeyDigit3)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}

	keyboard.press(ebiten.KeyS)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !g.entryActive {
		t.Fatal("s did not begin seed entry")
	}

	tickAtEntry := g.world.Tick
	for _, key := range []ebiten.Key{ebiten.KeyDigit4, ebiten.KeyDigit2, ebiten.KeyDigit9} {
		keyboard.press(key)
		if err := update(t, g); err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	if got := string(g.entryDigits); got != "429" {
		t.Fatalf("entry buffer is %q, want \"429\"", got)
	}
	if g.world.Tick != tickAtEntry {
		t.Errorf("the world advanced from %d to %d while a seed was being typed",
			tickAtEntry, g.world.Tick)
	}
	if g.ticksPerFrame != 100 {
		t.Errorf("typing digits changed the speed to %d", g.ticksPerFrame)
	}

	keyboard.press(ebiten.KeyBackspace)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	keyboard.press(ebiten.KeyEnter)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}

	if g.entryActive {
		t.Error("enter did not leave seed entry")
	}
	if g.seed != 42 {
		t.Errorf("committed seed %d, want 42", g.seed)
	}
	if g.world.Seed != 42 {
		t.Errorf("the new world has seed %d, want 42", g.world.Seed)
	}
	// The committing frame starts the new run at the speed already selected,
	// so it is worth exactly one frame of ticks and no more.
	if g.world.Tick != 100 {
		t.Errorf("the new run is at tick %d after its first frame at 100x, want 100", g.world.Tick)
	}
	if history := g.recorder.History(); len(history) != 100 {
		t.Errorf("the new run carries %d ticks of history, want 100", len(history))
	}
}

// TestSeedEntryEscapeKeepsTheRun is the other half of the mode: cancelling must
// leave the run exactly where it was, not restart it on the digits typed so far.
func TestSeedEntryEscapeKeepsTheRun(t *testing.T) {
	g, keyboard := newTestGame(t)

	keyboard.press(ebiten.KeyDigit2)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	tickBefore := g.world.Tick

	keyboard.press(ebiten.KeyS)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	keyboard.press(ebiten.KeyDigit7)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}
	keyboard.press(ebiten.KeyEscape)
	if err := update(t, g); err != nil {
		t.Fatalf("update: %v", err)
	}

	if g.entryActive {
		t.Error("escape did not leave seed entry")
	}
	if g.seed != 1 || g.world.Seed != 1 {
		t.Errorf("cancelled entry changed the seed to %d (world seed %d)", g.seed, g.world.Seed)
	}
	// The run must have carried on from where it was, not started over: a
	// cancelled entry that quietly restarted on seed 7 would look almost
	// identical on screen.
	if g.world.Tick <= tickBefore {
		t.Errorf("cancelled entry rewound the run (tick %d -> %d)", tickBefore, g.world.Tick)
	}
}

// TestQuitTerminates checks the quit key returns ebiten's termination sentinel
// rather than a real error, which is what stops the loop cleanly.
func TestQuitTerminates(t *testing.T) {
	g, keyboard := newTestGame(t)

	keyboard.press(ebiten.KeyQ)
	if err := g.Update(); err != ebiten.Termination {
		t.Fatalf("q returned %v, want ebiten.Termination", err)
	}
}

// TestRewindReproducesTheExactEarlierState is the claim the whole scrub design
// rests on: going back is a re-simulation, so the state at tick N after a
// rewind must be bit-identical to the state at tick N on the way out.
func TestRewindReproducesTheExactEarlierState(t *testing.T) {
	g, _ := newTestGame(t)

	hashes := map[int32]uint64{}
	for g.world.Tick < 1500 {
		g.step()
		if g.world.Tick%250 == 0 {
			hashes[g.world.Tick] = sim.Hash(g.world)
		}
	}

	// Backwards, including one target before the first snapshot (which forces
	// the rebuild-from-seed path) and one exactly on a snapshot boundary.
	for _, target := range []int32{1250, 1000, 500, 250} {
		g.rewind(target)

		if g.world.Tick != target {
			t.Fatalf("rewind to %d landed on tick %d", target, g.world.Tick)
		}
		if got, want := sim.Hash(g.world), hashes[target]; got != want {
			t.Fatalf("rewind to %d gave hash %016x, want %016x", target, got, want)
		}
		if got := len(g.recorder.History()); got != int(target) {
			t.Fatalf("rewind to %d left %d ticks of history", target, got)
		}
		if got, want := g.recorder.FinalPopulation(), int(g.world.Population()); got != want {
			t.Fatalf("rewind to %d: classifier population %d, world population %d",
				target, got, want)
		}
	}
}

// TestScrubForwardThenBackIsStable covers the interaction between the snapshot
// ring and repeated scrubbing: replaying over a stretch that already has
// snapshots must not corrupt them, so the second visit to a tick must hash the
// same as the first.
func TestScrubForwardThenBackIsStable(t *testing.T) {
	g, _ := newTestGame(t)

	for g.world.Tick < 2200 {
		g.step()
	}
	reference := sim.Hash(g.world)

	for round := 0; round < 3; round++ {
		g.scrub(-scrubFastTicks)
		g.scrub(scrubFastTicks)

		if g.world.Tick != 2200 {
			t.Fatalf("round %d: scrubbing round trip landed on tick %d", round, g.world.Tick)
		}
		if got := sim.Hash(g.world); got != reference {
			t.Fatalf("round %d: hash %016x after a round trip, want %016x", round, got, reference)
		}
	}
}

// TestScrubPausesAndClamps: scrubbing is for looking at a moment, so it must
// stop time, and it must not be able to walk off either end of the run.
func TestScrubPausesAndClamps(t *testing.T) {
	g, _ := newTestGame(t)

	g.scrub(-10 * scrubFastTicks)
	if !g.paused {
		t.Error("scrubbing did not pause the run")
	}
	if g.world.Tick != 0 {
		t.Errorf("scrubbing before the start landed on tick %d", g.world.Tick)
	}

	g.scrub(10 * int(g.options.Config.MaxTick))
	if g.world.Tick > g.options.Config.MaxTick {
		t.Errorf("scrubbing past the end landed on tick %d, beyond MaxTick %d",
			g.world.Tick, g.options.Config.MaxTick)
	}
}

// TestReplayVerdict covers the payoff of the determinism work: a replay checked
// against a real recorded hash. The expected value here is not hard-coded, it
// is produced by running the same seed and config the way the batch harness
// does, so this test also fails if the viewer's stopping rule ever diverges
// from the harness's.
func TestReplayVerdict(t *testing.T) {
	options := testOptions()

	reference := sim.NewWorld(options.Seed, options.Config)
	classifier := stats.NewClassifier(options.Classifier)
	for reference.Tick < options.Config.MaxTick {
		sim.Step(reference)
		classifier.Observe(int(reference.Population()))
		if stats.StopsRun(classifier.Outcome()) {
			break
		}
	}
	recorded := sim.Hash(reference)

	t.Run("match", func(t *testing.T) {
		matching := options
		matching.ExpectHash = recorded
		matching.HasExpect = true

		g := newGame(matching)
		g.input = newFakeKeyboard()

		if got := g.replayVerdict(sim.Hash(g.world)); got != verdictPending {
			t.Fatalf("verdict before the run finished is %v, want pending", got)
		}
		for !g.complete() {
			g.step()
		}
		if g.world.Tick != reference.Tick {
			t.Fatalf("the viewer stopped at tick %d, the harness at %d",
				g.world.Tick, reference.Tick)
		}
		if got := g.replayVerdict(sim.Hash(g.world)); got != verdictMatch {
			t.Fatalf("verdict is %v, want match", got)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		diverged := options
		diverged.ExpectHash = recorded ^ 1
		diverged.HasExpect = true

		g := newGame(diverged)
		g.input = newFakeKeyboard()
		for !g.complete() {
			g.step()
		}
		if got := g.replayVerdict(sim.Hash(g.world)); got != verdictMismatch {
			t.Fatalf("verdict is %v, want mismatch", got)
		}
	})

	t.Run("no expectation", func(t *testing.T) {
		g := newGame(options)
		g.input = newFakeKeyboard()
		if got := g.replayVerdict(sim.Hash(g.world)); got != verdictNone {
			t.Fatalf("verdict without --expect-hash is %v, want none", got)
		}
	})
}

// TestParseSeed covers the entry buffer's edge cases. A seed that does not fit
// in a uint64 must be refused outright: restarting on a silently truncated seed
// would produce a run the user did not ask for and cannot reproduce from what
// they typed.
func TestParseSeed(t *testing.T) {
	accepted := map[string]uint64{
		"0":                    0,
		"7":                    7,
		"18446744073709551615": ^uint64(0),
	}
	for text, want := range accepted {
		if got, ok := parseSeed([]byte(text)); !ok || got != want {
			t.Errorf("parseSeed(%q) = %d, %v; want %d, true", text, got, ok, want)
		}
	}

	for _, text := range []string{"", "18446744073709551616", "99999999999999999999"} {
		if got, ok := parseSeed([]byte(text)); ok {
			t.Errorf("parseSeed(%q) accepted, giving %d", text, got)
		}
	}
}
