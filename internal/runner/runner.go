package runner

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vukyn/worldbit/internal/sim"
	"github.com/vukyn/worldbit/internal/stats"
)

// nanosecondsPerTickEstimate is the per-tick cost used by EstimatedDuration.
//
// A 12 000-tick run at the default parameters costs about 350 ms on its own,
// which is the plan's figure. A pool saturating eight cores costs about 553 ms
// per run: the simulation is memory-bound over a 16 384-cell grid, so workers
// contend for bandwidth rather than scaling linearly. The saturated figure is
// the one used here, because the estimate exists to stop somebody launching a
// six-hour sweep blind, and for that purpose over-estimating a single-worker
// run is the harmless direction to be wrong in.
//
// It is a planning aid, not a benchmark: a cell whose population sits far above
// the default steady state costs materially more per tick, and a cell that goes
// extinct early costs far less.
const nanosecondsPerTickEstimate = 553 * 1000 * 1000 / 12000

// Cell is one point of a parameter sweep: a fully resolved, already-validated
// config plus the axis values that produced it. A plain seed batch is a single
// cell with no axes.
type Cell struct {
	// Index identifies the cell within the full Cartesian product of the
	// sweep, and survives cells being skipped as invalid.
	Index  int
	Config sim.Config
	Axes   []Axis
}

// Options describes a batch of runs.
type Options struct {
	// Cells is the parameter grid. Every cell runs the same seeds.
	Cells []Cell
	// SeedStart is the first seed; Seeds is how many consecutive seeds each
	// cell runs. Seeds are SeedStart … SeedStart+Seeds-1, which is legible and
	// easy to re-run one at a time.
	SeedStart uint64
	Seeds     int
	// Workers is the number of runs executed concurrently. Values below one
	// are treated as one; a value above the number of runs is capped, because
	// spare workers would only exit immediately.
	Workers int
	// Version is written into every record.
	Version string
	// HashEvery prints the canonical state hash every N ticks. It is a
	// single-run debugging aid: when set, the pool is reduced to one worker so
	// the trace stays legible and ordered.
	HashEvery int32
	// Log receives the hash trace. Nil discards it.
	Log io.Writer
}

// ClassifierParams maps a simulation config onto the classifier's thresholds.
//
// Only the parameters that genuinely cross the boundary travel: internal/stats
// never imports internal/sim, so that the classifier stays independently
// testable against hand-built series and the simulation stays free of
// statistics.
//
// The window length is deliberately NOT derived from TicksPerYear. An
// externally configured TicksPerYear could then push the window past the size
// the integer accumulators are proven safe for, turning a config value into a
// panic.
func ClassifierParams(cfg sim.Config) stats.ClassifierConfig {
	params := stats.DefaultClassifierConfig()
	params.TicksPerYear = int(cfg.TicksPerYear)
	params.OverrunPopulation = stats.OverrunPopulationFor(int(cfg.Width) * int(cfg.Height))
	params.BurnInWindows = int(cfg.BurnInWindows)
	params.MinOscillatingPopulation = int(cfg.MinOscillatingPopulation)
	return params
}

// TotalRuns is how many runs the options describe.
func TotalRuns(opts Options) int { return len(opts.Cells) * opts.Seeds }

// EstimatedDuration is an order-of-magnitude wall-time estimate for a batch,
// printed before it starts so that nobody launches a six-hour sweep blind.
//
// It assumes every run goes the full distance, which over-estimates cells that
// go extinct early, and it assumes the default per-tick cost, which
// under-estimates cells that sustain a large population.
func EstimatedDuration(opts Options) time.Duration {
	workers := effectiveWorkers(opts)
	if workers < 1 {
		return 0
	}

	var ticks int64
	for _, cell := range opts.Cells {
		ticks += int64(cell.Config.MaxTick) * int64(opts.Seeds)
	}
	return time.Duration(ticks * nanosecondsPerTickEstimate / int64(workers))
}

// effectiveWorkers resolves the requested worker count against the batch:
// never below one, never above the number of runs, and forced to one when a
// hash trace is being printed.
func effectiveWorkers(opts Options) int {
	workers := opts.Workers
	if workers < 1 {
		workers = 1
	}
	if opts.HashEvery > 0 {
		workers = 1
	}
	if total := TotalRuns(opts); total > 0 && workers > total {
		workers = total
	}
	return workers
}

type job struct {
	cell int
	seed uint64
}

// Run executes the batch and returns one record per run, sorted by cell index
// and then by seed.
//
// The returned order does not depend on the worker count: jobs are enumerated
// up front and each worker writes into its own slot of a preallocated slice, so
// there is no results channel to reorder and no mutex to contend on.
func Run(opts Options) ([]Record, error) {
	if len(opts.Cells) == 0 {
		return nil, errors.New("runner: no cells to run")
	}
	if opts.Seeds < 1 {
		return nil, fmt.Errorf("runner: Seeds must be positive, got %d", opts.Seeds)
	}

	jobs := make([]job, 0, TotalRuns(opts))
	for cellIndex := range opts.Cells {
		for offset := 0; offset < opts.Seeds; offset++ {
			jobs = append(jobs, job{cell: cellIndex, seed: opts.SeedStart + uint64(offset)})
		}
	}

	records := make([]Record, len(jobs))
	// One error slot per job rather than one per worker: the first failure by
	// JOB index is reported, so which worker happened to pick the job up
	// cannot change the error a batch fails with.
	failures := make([]error, len(jobs))

	var nextJob atomic.Int64
	var stopped atomic.Bool
	var group sync.WaitGroup

	workers := effectiveWorkers(opts)
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				index := int(nextJob.Add(1)) - 1
				if index >= len(jobs) || stopped.Load() {
					return
				}

				current := jobs[index]
				record, err := runOne(opts, opts.Cells[current.cell], current.seed)
				if err != nil {
					failures[index] = err
					// An invariant violation means the whole batch is
					// suspect; draining thousands of remaining runs would
					// only delay the report.
					stopped.Store(true)
					return
				}
				records[index] = record
			}
		}()
	}
	group.Wait()

	for _, err := range failures {
		if err != nil {
			return nil, err
		}
	}

	SortRecords(records)
	return records, nil
}

// runOne simulates one seed of one cell and turns it into a Record.
func runOne(opts Options, cell Cell, seed uint64) (Record, error) {
	cfg := cell.Config
	log := opts.Log
	if log == nil {
		log = io.Discard
	}

	started := time.Now()
	world := sim.NewWorld(seed, cfg)
	classifier := stats.NewClassifier(ClassifierParams(cfg))

	if err := world.VerifyError(); err != nil {
		return Record{}, fmt.Errorf("seed %d: invariant broken at tick %d: %w", seed, world.Tick, err)
	}

	for world.Tick < cfg.MaxTick {
		sim.Step(world)
		classifier.Observe(int(world.Population()))

		// Reported on the tick it happens: past the first violation every later
		// tick is built on known-bad state and only buries the cause.
		if err := world.VerifyError(); err != nil {
			return Record{}, fmt.Errorf("seed %d: invariant broken at tick %d: %w", seed, world.Tick, err)
		}
		if opts.HashEvery > 0 && world.Tick%opts.HashEvery == 0 {
			fmt.Fprintf(log, "seed=%d tick=%d hash=%016x\n", seed, world.Tick, sim.Hash(world))
		}

		if stats.StopsRun(classifier.Outcome()) {
			break
		}
	}

	outcome := classifier.Finish()

	return Record{
		Seed:        seed,
		Outcome:     outcome.String(),
		PeakPop:     classifier.PeakPopulation(),
		PeakYear:    classifier.PeakYear(),
		ExtinctYear: classifier.ExtinctYear(),
		FinalPop:    int(world.Population()),
		FinalTick:   int(world.Tick),
		StateHash:   sim.Hash(world),
		ConfigHash:  cfg.Hash(),
		Version:     opts.Version,
		WallMS:      time.Since(started).Milliseconds(),
		CellIndex:   cell.Index,
		Axes:        cell.Axes,
	}, nil
}
