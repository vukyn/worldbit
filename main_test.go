package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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

// TestExpectHashParsing covers the replay check's one piece of input handling.
//
// The rejection cases matter more than the acceptance ones: a mistyped
// --expect-hash that was quietly treated as "no expectation given" would turn a
// failed determinism check into a run that simply looked fine, which is the
// exact failure this whole project exists to make impossible.
//
// The dispatch this flag feeds — no --headless means the viewer — is covered in
// main_nogui_test.go, on the build where running it does not try to open a
// window.
func TestExpectHashParsing(t *testing.T) {
	accepted := []struct {
		text string
		want uint64
	}{
		{text: "", want: 0},
		{text: "0", want: 0},
		{text: "3f9a2b1c4d5e6f70", want: 0x3f9a2b1c4d5e6f70},
		{text: "0x3f9a2b1c4d5e6f70", want: 0x3f9a2b1c4d5e6f70},
		{text: "0XFFFFFFFFFFFFFFFF", want: 0xffffffffffffffff},
	}
	for _, testCase := range accepted {
		hash, present, err := parseExpectHash(testCase.text)
		if err != nil {
			t.Errorf("parseExpectHash(%q): %v", testCase.text, err)
			continue
		}
		if want := testCase.text != ""; present != want {
			t.Errorf("parseExpectHash(%q) present %v, want %v", testCase.text, present, want)
		}
		if hash != testCase.want {
			t.Errorf("parseExpectHash(%q) = %016x, want %016x", testCase.text, hash, testCase.want)
		}
	}

	rejected := []string{"nothex", "0x", "-1", "3f9a2b1c4d5e6f70f", "3f9a 2b1c", " 3f9a"}
	for _, text := range rejected {
		if _, present, err := parseExpectHash(text); err == nil {
			t.Errorf("parseExpectHash(%q) accepted the value (present %v); it is not a state hash",
				text, present)
		}
	}
}

// readCSV parses an output file into its header and rows.
func readCSV(t *testing.T, path string) ([]string, [][]string) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()

	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return rows[0], rows[1:]
}

func column(t *testing.T, header []string, name string) int {
	t.Helper()
	for index, heading := range header {
		if heading == name {
			return index
		}
	}
	t.Fatalf("no %q column in %v", name, header)
	return -1
}

// TestHeadlessRunWritesResolvedOutcomes covers the batch wiring end to end: the
// CSV must exist, hold one row per seed, and every row must carry a resolved
// verdict — never the placeholder RUNNING, which is what a classifier that was
// never fed or never finished would report.
func TestHeadlessRunWritesResolvedOutcomes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "runs.csv")

	err := newApp().Run([]string{
		"worldbit", "--headless", "--seed", "1", "--runs", "2", "--ticks", "1200", "--out", out,
	})
	if err != nil {
		t.Fatalf("headless run: %v", err)
	}

	header, rows := readCSV(t, out)
	if len(rows) != 2 {
		t.Fatalf("wrote %d rows, want 2", len(rows))
	}

	resolved := map[string]bool{}
	for _, outcome := range []stats.Outcome{
		stats.OutcomeExtinct, stats.OutcomeStable, stats.OutcomeOscillating,
		stats.OutcomeOverrun, stats.OutcomeDeclining, stats.OutcomeTimeout,
	} {
		resolved[outcome.String()] = true
	}

	outcomeColumn := column(t, header, "outcome")
	for _, row := range rows {
		if !resolved[row[outcomeColumn]] {
			t.Errorf("row %v reported outcome %q, want one of the six resolved outcomes",
				row, row[outcomeColumn])
		}
	}

	// The sidecar is what makes a results file reproducible on its own.
	sidecar := out + ".config.json"
	data, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("read sidecar %s: %v", sidecar, err)
	}
	if len(data) == 0 {
		t.Errorf("sidecar %s is empty", sidecar)
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

func writeSweep(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sweep.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write sweep: %v", err)
	}
	return path
}

// blankWallTimes zeroes the wall_ms column, the one non-deterministic field, so
// two result files can be compared byte for byte.
var wallTimeColumn = regexp.MustCompile(`(?m)^((?:[^,\n]*,){10})[0-9]+`)

func blankWallTimes(data []byte) []byte {
	return wallTimeColumn.ReplaceAll(data, []byte("${1}0"))
}

// TestSweepProducesOneRowPerCellPerSeed covers the size and shape of a sweep's
// output end to end, including the per-run axis columns that make each row
// self-describing.
func TestSweepProducesOneRowPerCellPerSeed(t *testing.T) {
	sweep := writeSweep(t, `{
		"seeds": 3,
		"seed_start": 1,
		"axes": {
			"InitFoodPerCell": [1, 2],
			"ReproEnergyCost": [30, 40]
		}
	}`)
	out := filepath.Join(t.TempDir(), "runs.csv")

	err := newApp().Run([]string{
		"worldbit", "--headless", "--sweep", sweep, "--ticks", "1200", "--out", out,
	})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	header, rows := readCSV(t, out)
	if len(rows) != 4*3 {
		t.Fatalf("wrote %d rows, want 12 (4 cells x 3 seeds)", len(rows))
	}

	foodColumn := column(t, header, "InitFoodPerCell")
	costColumn := column(t, header, "ReproEnergyCost")
	seen := map[string]int{}
	for _, row := range rows {
		seen[row[foodColumn]+"/"+row[costColumn]]++
	}
	for _, cell := range []string{"1/30", "1/40", "2/30", "2/40"} {
		if seen[cell] != 3 {
			t.Errorf("cell %s has %d rows, want 3", cell, seen[cell])
		}
	}

	// Every cell must appear once in the aggregate, which is the file a sweep
	// is actually read from.
	summaryHeader, summaryRows := readCSV(t, out+".cells.csv")
	if len(summaryRows) != 4 {
		t.Fatalf("aggregate has %d rows, want 4", len(summaryRows))
	}
	runsColumn := column(t, summaryHeader, "runs")
	for _, row := range summaryRows {
		if row[runsColumn] != "3" {
			t.Errorf("aggregate row %v reports %s runs, want 3", row, row[runsColumn])
		}
	}
}

// TestSweepOutputIsIdenticalAcrossWorkerCounts extends the runner's
// determinism guarantee to the whole sweep path: cell numbering, per-run rows
// and the aggregate must not depend on how many workers produced them.
func TestSweepOutputIsIdenticalAcrossWorkerCounts(t *testing.T) {
	sweep := writeSweep(t, `{
		"seeds": 2,
		"axes": {"InitFoodPerCell": [1, 2], "ReproEnergyCost": [30, 40]}
	}`)
	directory := t.TempDir()

	run := func(workers, index string) (string, string) {
		out := filepath.Join(directory, "runs"+index+".csv")
		err := newApp().Run([]string{
			"worldbit", "--headless", "--sweep", sweep, "--ticks", "1200",
			"--out", out, "--workers", workers,
		})
		if err != nil {
			t.Fatalf("sweep with %s workers: %v", workers, err)
		}
		return out, out + ".cells.csv"
	}

	serialRuns, serialCells := run("1", "1")
	parallelRuns, parallelCells := run("8", "8")

	for _, pair := range [][2]string{{serialRuns, parallelRuns}, {serialCells, parallelCells}} {
		first := blankWallTimes(readBytes(t, pair[0]))
		second := blankWallTimes(readBytes(t, pair[1]))
		if !bytes.Equal(first, second) {
			t.Errorf("%s and %s differ\n--- 1 worker ---\n%s\n--- 8 workers ---\n%s",
				pair[0], pair[1], first, second)
		}
	}
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// TestSweepUnknownAxisKeyIsAHardError checks that a typo in a sweep file stops
// the program rather than silently producing a grid of identical cells.
func TestSweepUnknownAxisKeyIsAHardError(t *testing.T) {
	sweep := writeSweep(t, `{"seeds": 1, "axes": {"InitFoodPerCel": [1, 2]}}`)
	out := filepath.Join(t.TempDir(), "runs.csv")

	err := newApp().Run([]string{"worldbit", "--headless", "--sweep", sweep, "--out", out})
	if err == nil {
		t.Fatal("an unknown axis key was accepted")
	}
	if !strings.Contains(err.Error(), "InitFoodPerCel") {
		t.Errorf("error %q does not name the offending key", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("an output file was written for a sweep that never ran")
	}
}

// TestSweepSkipsInvalidCellsAndRunsTheRest is the difference between a wide
// grid being usable and being all-or-nothing: the impossible corner is dropped
// with a message, the rest still runs.
func TestSweepSkipsInvalidCellsAndRunsTheRest(t *testing.T) {
	// ReproEnergyCost above the default ReproEnergyMin of 60 is invalid.
	sweep := writeSweep(t, `{
		"seeds": 1,
		"axes": {"InitFoodPerCell": [1, 2], "ReproEnergyCost": [40, 80]}
	}`)
	out := filepath.Join(t.TempDir(), "runs.csv")

	err := newApp().Run([]string{
		"worldbit", "--headless", "--sweep", sweep, "--ticks", "1200", "--out", out,
	})
	if err != nil {
		t.Fatalf("an invalid cell aborted the whole sweep: %v", err)
	}

	_, rows := readCSV(t, out)
	if len(rows) != 2 {
		t.Fatalf("wrote %d rows, want 2 (the 2 valid cells x 1 seed)", len(rows))
	}
}

// TestSweepWithEveryCellInvalidFails draws the line: skipping is for a corner
// of the grid, not for the whole thing. A sweep that runs nothing must say so
// rather than write an empty results file.
func TestSweepWithEveryCellInvalidFails(t *testing.T) {
	sweep := writeSweep(t, `{"seeds": 1, "axes": {"ReproEnergyCost": [70, 80]}}`)
	out := filepath.Join(t.TempDir(), "runs.csv")

	err := newApp().Run([]string{"worldbit", "--headless", "--sweep", sweep, "--out", out})
	if err == nil {
		t.Fatal("a sweep with no runnable cells reported success")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error %q does not explain that every cell was invalid", err)
	}
}
