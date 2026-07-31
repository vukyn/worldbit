package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/vukyn/worldbit/internal/sim"
	"github.com/vukyn/worldbit/internal/stats"
)

// resolveFromArgs drives the real flag set so the test exercises the same
// parsing path as the binary, not a hand-built cli.Context.
func resolveFromArgs(t *testing.T, args ...string) sim.Config {
	t.Helper()

	var resolved sim.Config
	app := &cli.App{
		Name:  "worldbit",
		Flags: appFlags(),
		Action: func(c *cli.Context) error {
			var err error
			resolved, err = resolveConfig(c)
			return err
		},
	}

	if err := app.Run(append([]string{"worldbit"}, args...)); err != nil {
		t.Fatalf("resolve %v: %v", args, err)
	}
	return resolved
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestFlagResolutionOrder pins the precedence rule that is easiest to break by
// accident: --ticks and --verify shadow Config fields, so they must only
// override when explicitly present. A naive unconditional assignment would let
// the flag's own default silently clobber a value set in the config file, and
// the run would look perfectly normal while ignoring the file.
func TestFlagResolutionOrder(t *testing.T) {
	defaults := sim.DefaultConfig()

	t.Run("no flags gives defaults", func(t *testing.T) {
		if got := resolveFromArgs(t); got != defaults {
			t.Errorf("resolved %+v, want the defaults", got)
		}
	})

	t.Run("ticks flag sets MaxTick", func(t *testing.T) {
		if got := resolveFromArgs(t, "--ticks", "500").MaxTick; got != 500 {
			t.Errorf("MaxTick = %d, want 500", got)
		}
	})

	t.Run("config file survives an unset ticks flag", func(t *testing.T) {
		path := writeConfig(t, `{"MaxTick": 777, "Verify": true}`)
		got := resolveFromArgs(t, "--config", path)
		if got.MaxTick != 777 {
			t.Errorf("MaxTick = %d, want 777 — the ticks flag default clobbered the file", got.MaxTick)
		}
		if !got.Verify {
			t.Error("Verify = false, want true — the verify flag default clobbered the file")
		}
	})

	t.Run("explicit flags win over the file", func(t *testing.T) {
		path := writeConfig(t, `{"MaxTick": 777, "Verify": true}`)
		got := resolveFromArgs(t, "--config", path, "--ticks", "300", "--verify=false")
		if got.MaxTick != 300 {
			t.Errorf("MaxTick = %d, want 300", got.MaxTick)
		}
		if got.Verify {
			t.Error("Verify = true, want false")
		}
	})

	t.Run("set wins over the file", func(t *testing.T) {
		path := writeConfig(t, `{"FoodMax": 9}`)
		if got := resolveFromArgs(t, "--config", path, "--set", "FoodMax=2").FoodMax; got != 2 {
			t.Errorf("FoodMax = %d, want 2", got)
		}
	})

	t.Run("short aliases resolve the same way", func(t *testing.T) {
		if got := resolveFromArgs(t, "-t", "640").MaxTick; got != 640 {
			t.Errorf("MaxTick = %d, want 640", got)
		}
	})
}

// TestInvalidConfigFailsBeforeRunning checks that a bad parameter aborts with
// the offending field named, rather than producing a plausible-looking run.
func TestInvalidConfigFailsBeforeRunning(t *testing.T) {
	err := newApp().Run([]string{"worldbit", "--headless", "--set", "Width=100"})
	if err == nil {
		t.Fatal("an invalid Width was accepted")
	}

	var configError *sim.ConfigError
	if !errors.As(err, &configError) {
		t.Fatalf("got %T (%v), want *sim.ConfigError", err, err)
	}
	if configError.Field != "Width" {
		t.Errorf("blamed %q, want %q", configError.Field, "Width")
	}
}

// TestGUIDispatchIsStubbed documents the current mode dispatch: without
// --headless the run goes to runGUI, which is a stub in both build-tag
// variants until the viewer lands.
func TestGUIDispatchIsStubbed(t *testing.T) {
	err := newApp().Run([]string{"worldbit", "--seed", "3"})
	if err == nil {
		t.Fatal("the GUI stub reported success")
	}
}

// captureStdout collects everything fn prints. The run summary goes to stdout
// via fmt.Printf, so this is the only way to assert on it without threading a
// writer through the whole command.
//
// Safe only for small outputs: the pipe is drained after fn returns, so more
// than a pipe buffer's worth would deadlock. Every caller here runs a short
// simulation with --hash-every off.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original }()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	captured, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read captured output: %v", err)
	}
	return string(captured)
}

// TestHeadlessRunReportsAnOutcome covers the classifier wiring end to end: a
// real run must finish with a resolved verdict, never with the placeholder
// RUNNING, which is what a classifier that was never fed or never finished
// would report.
func TestHeadlessRunReportsAnOutcome(t *testing.T) {
	var runError error
	output := captureStdout(t, func() {
		runError = newApp().Run([]string{"worldbit", "--headless", "--seed", "1", "--ticks", "1200"})
	})
	if runError != nil {
		t.Fatalf("headless run: %v", runError)
	}

	index := strings.Index(output, "outcome=")
	if index < 0 {
		t.Fatalf("no outcome in the run summary:\n%s", output)
	}
	reported := strings.Fields(output[index+len("outcome="):])[0]

	resolved := []stats.Outcome{
		stats.OutcomeExtinct, stats.OutcomeStable, stats.OutcomeOscillating,
		stats.OutcomeOverrun, stats.OutcomeDeclining, stats.OutcomeTimeout,
	}
	for _, outcome := range resolved {
		if reported == outcome.String() {
			return
		}
	}
	t.Errorf("run reported outcome %q, want one of the six resolved outcomes", reported)
}

// TestClassifierParamsFollowTheSimulationGrid pins the two numbers that cross
// the sim/stats boundary, and — more importantly — pins that the window length
// does NOT. Deriving the window from TicksPerYear would let an externally
// configured value push it past the size the integer accumulators are proven
// safe for, turning a config knob into a panic.
func TestClassifierParamsFollowTheSimulationGrid(t *testing.T) {
	cfg := sim.DefaultConfig()
	params := classifierParams(cfg)

	if params.TicksPerYear != int(cfg.TicksPerYear) {
		t.Errorf("TicksPerYear = %d, want %d", params.TicksPerYear, cfg.TicksPerYear)
	}
	if want := stats.OverrunPopulationFor(int(cfg.Width) * int(cfg.Height)); params.OverrunPopulation != want {
		t.Errorf("OverrunPopulation = %d, want %d", params.OverrunPopulation, want)
	}

	smallGrid := sim.DefaultConfig()
	smallGrid.Width, smallGrid.Height = 64, 64
	smallGrid.TicksPerYear = 100_000
	smallParams := classifierParams(smallGrid)

	if want := stats.OverrunPopulationFor(64 * 64); smallParams.OverrunPopulation != want {
		t.Errorf("OverrunPopulation on a 64x64 grid = %d, want %d", smallParams.OverrunPopulation, want)
	}
	if want := stats.DefaultClassifierConfig().WindowTicks; smallParams.WindowTicks != want {
		t.Errorf("WindowTicks = %d with TicksPerYear %d, want the fixed %d",
			smallParams.WindowTicks, smallGrid.TicksPerYear, want)
	}
	if smallParams.WindowTicks > stats.MaxWindowSize {
		t.Errorf("WindowTicks = %d exceeds MaxWindowSize %d", smallParams.WindowTicks, stats.MaxWindowSize)
	}
}

// TestDumpConfigWritesResolvedConfig checks that --dump-config records the
// resolved config (defaults plus overrides), which is the canonical record of
// an experiment.
func TestDumpConfigWritesResolvedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dumped.json")

	err := newApp().Run([]string{"worldbit", "--set", "FoodMax=8", "--dump-config", path})
	if err != nil {
		t.Fatalf("dump-config: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dumped config: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("dumped config is empty")
	}

	reloaded := resolveFromArgs(t, "--config", path)
	if reloaded.FoodMax != 8 {
		t.Errorf("FoodMax = %d after a dump/reload, want 8", reloaded.FoodMax)
	}
	expected := sim.DefaultConfig()
	expected.FoodMax = 8
	if reloaded.Hash() != expected.Hash() {
		t.Errorf("dump/reload changed the config hash: %016x != %016x", reloaded.Hash(), expected.Hash())
	}
}
