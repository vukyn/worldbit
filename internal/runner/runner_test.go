package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vukyn/worldbit/internal/sim"
	"github.com/vukyn/worldbit/internal/stats"
)

// shortConfig keeps the tests to a few seconds. The tick limit is four windows,
// which is one more than a run needs to resolve STABLE past the default
// burn-in, so the classifier is genuinely exercised rather than always timing
// out.
func shortConfig() sim.Config {
	cfg := sim.DefaultConfig()
	cfg.MaxTick = 4 * int32(stats.DefaultClassifierConfig().WindowTicks)
	return cfg
}

func baseOptions(cfg sim.Config, seeds int) Options {
	return Options{
		Cells:     []Cell{{Index: 0, Config: cfg}},
		SeedStart: 1,
		Seeds:     seeds,
		Workers:   1,
		Version:   "test",
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// TestClassifierParamsFollowTheSimulationGrid pins the numbers that cross the
// sim/stats boundary, and — more importantly — pins that the window length does
// NOT. Deriving the window from TicksPerYear would let an externally configured
// value push it past the size the integer accumulators are proven safe for,
// turning a config knob into a panic.
func TestClassifierParamsFollowTheSimulationGrid(t *testing.T) {
	cfg := sim.DefaultConfig()
	params := ClassifierParams(cfg)

	if params.TicksPerYear != int(cfg.TicksPerYear) {
		t.Errorf("TicksPerYear = %d, want %d", params.TicksPerYear, cfg.TicksPerYear)
	}
	if want := stats.OverrunPopulationFor(int(cfg.Width) * int(cfg.Height)); params.OverrunPopulation != want {
		t.Errorf("OverrunPopulation = %d, want %d", params.OverrunPopulation, want)
	}
	if params.BurnInWindows != int(cfg.BurnInWindows) {
		t.Errorf("BurnInWindows = %d, want %d", params.BurnInWindows, cfg.BurnInWindows)
	}

	smallGrid := sim.DefaultConfig()
	smallGrid.Width, smallGrid.Height = 64, 64
	smallGrid.TicksPerYear = 100_000
	smallParams := ClassifierParams(smallGrid)

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

// TestRunnerParallelMatchesSerial is the proof that the parallelism is only
// between runs. One worker and eight workers must produce byte-identical
// files, in both formats — if any shared state leaked into the simulation, or
// if results were emitted in completion order, this is where it would show.
func TestRunnerParallelMatchesSerial(t *testing.T) {
	directory := t.TempDir()
	cfg := shortConfig()

	for _, extension := range []string{".csv", ".json"} {
		t.Run(extension, func(t *testing.T) {
			serialOptions := baseOptions(cfg, 8)
			serialOptions.Workers = 1
			serial, err := Run(serialOptions)
			if err != nil {
				t.Fatalf("serial run: %v", err)
			}

			parallelOptions := baseOptions(cfg, 8)
			parallelOptions.Workers = 8
			parallel, err := Run(parallelOptions)
			if err != nil {
				t.Fatalf("parallel run: %v", err)
			}

			serialPath := filepath.Join(directory, "serial"+extension)
			parallelPath := filepath.Join(directory, "parallel"+extension)
			if err := Write(serialPath, serial, "test", cfg.Hash()); err != nil {
				t.Fatalf("write serial: %v", err)
			}
			if err := Write(parallelPath, parallel, "test", cfg.Hash()); err != nil {
				t.Fatalf("write parallel: %v", err)
			}

			// wall_ms is the one non-deterministic column, so it is blanked
			// before the comparison rather than excluded from the schema.
			serialBytes := blankWallTimes(t, readFile(t, serialPath))
			parallelBytes := blankWallTimes(t, readFile(t, parallelPath))
			if !bytes.Equal(serialBytes, parallelBytes) {
				t.Errorf("one worker and eight workers produced different files\n--- 1 worker ---\n%s\n--- 8 workers ---\n%s",
					serialBytes, parallelBytes)
			}
		})
	}
}

// TestOutputSortedBySeed feeds the writer records in the worst possible order
// and requires the file to come out sorted. Sorting must live in the writer,
// not only in Run, or a caller that assembles records itself would silently
// emit an unsortable file.
func TestOutputSortedBySeed(t *testing.T) {
	records := []Record{
		{Seed: 30, Outcome: "STABLE", CellIndex: 1},
		{Seed: 10, Outcome: "STABLE", CellIndex: 1},
		{Seed: 20, Outcome: "STABLE", CellIndex: 0},
		{Seed: 5, Outcome: "STABLE", CellIndex: 1},
		{Seed: 10, Outcome: "STABLE", CellIndex: 0},
	}

	path := filepath.Join(t.TempDir(), "runs.csv")
	if err := Write(path, records, "test", 0); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The input slice must not be reordered underneath the caller.
	if records[0].Seed != 30 {
		t.Errorf("Write reordered the caller's slice: first seed is now %d", records[0].Seed)
	}

	rows := csvRows(t, readFile(t, path))
	seedColumn := 0
	got := make([]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		got = append(got, row[seedColumn])
	}

	// Cell 0's seeds first, then cell 1's, each ascending.
	want := []string{"10", "20", "5", "10", "30"}
	if len(got) != len(want) {
		t.Fatalf("wrote %d rows, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("row order %v, want %v (cell index, then seed)", got, want)
		}
	}
}

// TestEarlyStopDoesNotChangeTheOutcome covers the optimisation that makes a
// sweep affordable. EXTINCT and OVERRUN end a run where they happen, saving the
// remaining ticks; the verdict must be exactly what running to MaxTick would
// have produced.
//
// The test also insists that at least one case actually stops early. Without
// that, a change that quietly disabled the early stop would leave the test
// passing while proving nothing.
func TestEarlyStopDoesNotChangeTheOutcome(t *testing.T) {
	extinct := shortConfig()
	// A tiny board with an expensive search and no standing food: the founders
	// starve before anything matures.
	extinct.Width, extinct.Height = 32, 32
	extinct.InitFoodPerCell = 1
	extinct.FoodRegrowTicks = 1000
	extinct.InitAgents = 40

	overrun := shortConfig()
	// A full pantry on a small board: the first boom passes 40 % of the cells.
	overrun.Width, overrun.Height = 32, 32
	overrun.InitFoodPerCell = 5
	overrun.FoodRegrowTicks = 20
	overrun.InitAgents = 200

	configs := []struct {
		name string
		cfg  sim.Config
	}{
		{"default", shortConfig()},
		{"starving", extinct},
		{"overrunning", overrun},
	}

	sawEarlyStop := false
	for _, testCase := range configs {
		if err := testCase.cfg.Validate(); err != nil {
			t.Fatalf("%s config is invalid: %v", testCase.name, err)
		}

		early, err := Run(baseOptions(testCase.cfg, 4))
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		full := runToLimit(t, testCase.cfg, 4)

		for index := range early {
			if early[index].Outcome != full[index].Outcome {
				t.Errorf("%s seed %d: early stop gave %s, running to MaxTick gave %s",
					testCase.name, early[index].Seed, early[index].Outcome, full[index].Outcome)
			}
			if early[index].PeakPop != full[index].PeakPop {
				t.Errorf("%s seed %d: early stop gave peak %d, running to MaxTick gave %d",
					testCase.name, early[index].Seed, early[index].PeakPop, full[index].PeakPop)
			}
			if early[index].FinalTick < full[index].FinalTick {
				sawEarlyStop = true
			}
		}
	}

	if !sawEarlyStop {
		t.Error("no run stopped early, so this test proved nothing — the configurations no " +
			"longer reach a terminal outcome before MaxTick")
	}
}

// runToLimit reproduces a batch with the early stop disabled, by classifying
// every tick to MaxTick regardless of the verdict.
func runToLimit(t *testing.T, cfg sim.Config, seeds int) []Record {
	t.Helper()

	records := make([]Record, 0, seeds)
	for offset := 0; offset < seeds; offset++ {
		seed := uint64(1 + offset)
		world := sim.NewWorld(seed, cfg)
		classifier := stats.NewClassifier(ClassifierParams(cfg))
		for world.Tick < cfg.MaxTick {
			sim.Step(world)
			classifier.Observe(int(world.Population()))
		}
		outcome := classifier.Finish()
		records = append(records, Record{
			Seed:      seed,
			Outcome:   outcome.String(),
			PeakPop:   classifier.PeakPopulation(),
			FinalPop:  int(world.Population()),
			FinalTick: int(world.Tick),
		})
	}
	return records
}

// TestRunRejectsAnEmptyBatch keeps a misconfigured batch from silently writing
// an empty results file that looks like a run nothing interesting happened in.
func TestRunRejectsAnEmptyBatch(t *testing.T) {
	if _, err := Run(Options{Seeds: 1}); err == nil {
		t.Error("a batch with no cells was accepted")
	}
	if _, err := Run(baseOptions(shortConfig(), 0)); err == nil {
		t.Error("a batch with no seeds was accepted")
	}
}

// TestHashEveryForcesASingleWorker pins the one place the runner overrides the
// caller: a hash trace interleaved across eight workers is unreadable, and
// reading it is the only reason the flag exists.
func TestHashEveryForcesASingleWorker(t *testing.T) {
	cfg := shortConfig()
	cfg.MaxTick = 200

	var trace bytes.Buffer
	options := baseOptions(cfg, 2)
	options.Workers = 8
	options.HashEvery = 100
	options.Log = &trace

	if got := effectiveWorkers(options); got != 1 {
		t.Errorf("effectiveWorkers = %d with a hash trace, want 1", got)
	}
	if _, err := Run(options); err != nil {
		t.Fatalf("run: %v", err)
	}
	if lines := bytes.Count(trace.Bytes(), []byte("\n")); lines != 4 {
		t.Errorf("traced %d lines, want 4 (2 seeds x 2 checkpoints):\n%s", lines, trace.String())
	}
}

// TestEstimatedDurationScales covers the planning estimate. It is the only
// thing standing between a user and an accidentally six-hour sweep, so it must
// at least grow with the work and shrink with the workers.
func TestEstimatedDurationScales(t *testing.T) {
	cfg := sim.DefaultConfig()
	one := Options{Cells: []Cell{{Config: cfg}}, Seeds: 1, Workers: 1}

	if TotalRuns(one) != 1 {
		t.Errorf("TotalRuns = %d, want 1", TotalRuns(one))
	}
	single := EstimatedDuration(one)
	if single <= 0 {
		t.Fatalf("estimate for one run is %v, want a positive duration", single)
	}

	tenCells := one
	tenCells.Cells = make([]Cell, 10)
	for index := range tenCells.Cells {
		tenCells.Cells[index] = Cell{Index: index, Config: cfg}
	}
	tenCells.Seeds = 10
	if TotalRuns(tenCells) != 100 {
		t.Errorf("TotalRuns = %d, want 100", TotalRuns(tenCells))
	}
	if want := 100 * single; EstimatedDuration(tenCells) != want {
		t.Errorf("estimate for 100 runs is %v, want %v", EstimatedDuration(tenCells), want)
	}

	// Workers are capped at the number of runs, so eight workers over one run
	// must not divide the estimate by eight.
	overSubscribed := one
	overSubscribed.Workers = 8
	if got := EstimatedDuration(overSubscribed); got != single {
		t.Errorf("estimate for one run on eight workers is %v, want %v", got, single)
	}

	parallel := tenCells
	parallel.Workers = 4
	if want := EstimatedDuration(tenCells) / 4; EstimatedDuration(parallel) != want {
		t.Errorf("estimate on four workers is %v, want %v", EstimatedDuration(parallel), want)
	}
}
