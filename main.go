// Command worldbit is a deterministic agent-based world simulator.
//
// The primary workflow is headless: run N seeds, classify each run's outcome,
// emit one record per seed, then replay an interesting seed in the GUI by
// re-simulating it from the seed. That replay is only possible because the
// simulation is bit-exact deterministic — determinism is the product feature,
// not a testing detail.
package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/vukyn/worldbit/internal/config"
	"github.com/vukyn/worldbit/internal/runner"
	"github.com/vukyn/worldbit/internal/sim"
)

// version is written into every output record, so a "same seed, different
// hash" investigation can start from the binary rather than from guesswork.
var version = "0.1.0"

func main() {
	if err := newApp().Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "worldbit:", err)
		os.Exit(1)
	}
}

func newApp() *cli.App {
	return &cli.App{
		Name:    "worldbit",
		Usage:   "Deterministic agent-based world simulator",
		Version: version,
		Flags:   appFlags(),
		Action:  run,
	}
}

func appFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:    "headless",
			Aliases: []string{"H"},
			Usage:   "run the batch harness instead of the GUI",
			Value:   false,
		},
		&cli.Uint64Flag{
			Name:    "seed",
			Aliases: []string{"s"},
			Usage:   "world seed; in batch mode the first of --runs consecutive seeds",
			Value:   1,
		},
		&cli.IntFlag{
			Name:    "runs",
			Aliases: []string{"n"},
			Usage:   "number of consecutive seeds to run (batch only)",
			Value:   1,
		},
		&cli.IntFlag{
			Name:    "ticks",
			Aliases: []string{"t"},
			Usage:   "MaxTick; 12000 batch / 120000 deep",
			Value:   int(sim.DefaultConfig().MaxTick),
		},
		&cli.StringFlag{
			Name:    "out",
			Aliases: []string{"o"},
			Usage:   "output path; .csv or .json selects the writer",
			Value:   "runs.csv",
		},
		&cli.IntFlag{
			Name:    "workers",
			Aliases: []string{"w"},
			Usage:   "batch parallelism",
			Value:   runtime.GOMAXPROCS(0),
		},
		&cli.StringFlag{
			Name:    "config",
			Aliases: []string{"c"},
			Usage:   "JSON config file; absent fields keep their defaults",
		},
		&cli.StringSliceFlag{
			Name:  "set",
			Usage: "single-parameter override, repeatable: --set InitFoodPerCell=3 --set FoodMax=8",
		},
		&cli.StringFlag{
			Name:  "sweep",
			Usage: "JSON parameter-grid file; runs every cell of the grid over --seeds seeds (batch only)",
		},
		&cli.StringFlag{
			Name:  "dump-config",
			Usage: "write the resolved config as JSON to this path and exit",
		},
		&cli.BoolFlag{
			Name:  "verify",
			Usage: "enable simulation invariant assertions (slow)",
			Value: false,
		},
		&cli.IntFlag{
			Name:  "hash-every",
			Usage: "print the canonical state hash every N ticks (0 = off)",
			Value: 0,
		},
		&cli.IntFlag{
			Name:  "scale",
			Usage: "GUI pixel scale (the grid is rendered at this many pixels per cell)",
			Value: 4,
		},
	}
}

func run(c *cli.Context) error {
	cfg, err := resolveConfig(c)
	if err != nil {
		return err
	}

	// Validation runs before anything else, so an invalid external config
	// fails loudly with the offending field named rather than producing a
	// plausible-looking but meaningless run.
	if err := cfg.Validate(); err != nil {
		return err
	}

	if path := c.String("dump-config"); path != "" {
		if err := config.Dump(cfg, path); err != nil {
			return err
		}
		fmt.Printf("wrote %s (config_hash %016x)\n", path, cfg.Hash())
		return nil
	}

	if c.Bool("headless") {
		return runHeadless(c, cfg)
	}
	return runGUI(cfg, c.Uint64("seed"), c.Int("scale"))
}

// resolveConfig applies the resolution order: defaults, then the --config
// file, then --set overrides, then the flags that shadow a Config field.
//
// --ticks and --verify are Config fields with their own flags, so they only
// override when explicitly present on the command line; otherwise their flag
// default would silently clobber a value set in the config file.
func resolveConfig(c *cli.Context) (sim.Config, error) {
	cfg, err := config.Resolve(c.String("config"), c.StringSlice("set"))
	if err != nil {
		return cfg, err
	}

	if c.IsSet("ticks") {
		cfg.MaxTick = int32(c.Int("ticks"))
	}
	if c.IsSet("verify") {
		cfg.Verify = c.Bool("verify")
	}

	return cfg, nil
}

// runHeadless is the batch entry point: it resolves the grid to run, reports
// its size before starting, runs it, and writes the results.
func runHeadless(c *cli.Context, cfg sim.Config) error {
	options := runner.Options{
		Cells:     []runner.Cell{{Index: 0, Config: cfg}},
		SeedStart: c.Uint64("seed"),
		Seeds:     c.Int("runs"),
		Workers:   c.Int("workers"),
		Version:   version,
		HashEvery: int32(c.Int("hash-every")),
		Log:       os.Stdout,
	}

	sweeping := c.String("sweep") != ""
	if sweeping {
		var err error
		if options, err = applySweep(c, cfg, options); err != nil {
			return err
		}
	}
	if options.Seeds < 1 {
		return fmt.Errorf("--runs must be positive, got %d", options.Seeds)
	}

	reportPlan(options)

	records, err := runner.Run(options)
	if err != nil {
		return err
	}

	out := c.String("out")
	if err := runner.Write(out, records, version, cfg.Hash()); err != nil {
		return err
	}
	if err := config.Dump(cfg, runner.SidecarPath(out)); err != nil {
		return err
	}
	fmt.Printf("wrote %s and %s\n", out, runner.SidecarPath(out))

	if sweeping {
		summaryPath := runner.CellSummaryPath(out)
		if err := runner.WriteCellSummary(summaryPath, records); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%d cells)\n", summaryPath, len(runner.SummariseCells(records)))
	}

	fmt.Printf("outcomes: %s\n", runner.FormatOutcomeCounts(runner.OutcomeCounts(records)))
	if cfg.Verify {
		fmt.Println("invariants held on every tick of every run")
	}
	return nil
}

// applySweep replaces the single base cell with the sweep's grid. The sweep
// file governs the seed range, because "seeds" and "seed_start" live in it;
// --runs and --seed are reported as ignored rather than silently overridden.
func applySweep(c *cli.Context, cfg sim.Config, options runner.Options) (runner.Options, error) {
	sweep, err := config.LoadSweep(c.String("sweep"))
	if err != nil {
		return options, err
	}

	if c.IsSet("runs") || c.IsSet("seed") {
		fmt.Fprintln(os.Stderr,
			"worldbit: --seed/--runs are ignored under --sweep; the sweep file's seeds and "+
				"seed_start govern the seed range")
	}

	expanded, skipped := sweep.Expand(cfg)
	for _, cell := range skipped {
		fmt.Fprintf(os.Stderr, "worldbit: skipping cell %d (%s): %v\n",
			cell.Index, config.DescribeAxes(cell.Axes), cell.Err)
	}
	if len(skipped) > 0 {
		fmt.Fprintf(os.Stderr, "worldbit: skipped %d of %d cells as invalid\n",
			len(skipped), sweep.CellCount())
	}
	if len(expanded) == 0 {
		return options, fmt.Errorf("sweep: all %d cells were invalid; nothing to run", sweep.CellCount())
	}

	options.Cells = make([]runner.Cell, len(expanded))
	for i, cell := range expanded {
		axes := make([]runner.Axis, len(cell.Axes))
		for j, axis := range cell.Axes {
			axes[j] = runner.Axis{Name: axis.Name, Value: axis.Value}
		}
		options.Cells[i] = runner.Cell{Index: cell.Index, Config: cell.Config, Axes: axes}
	}
	options.SeedStart = sweep.SeedStart
	options.Seeds = sweep.Seeds

	return options, nil
}

// reportPlan prints the size of the batch and an estimate of how long it will
// take BEFORE it starts. There are no silent caps in this harness, so the one
// protection against accidentally launching a six-hour sweep is knowing that is
// what you launched.
func reportPlan(options runner.Options) {
	if options.HashEvery > 0 && options.Workers > 1 {
		fmt.Fprintln(os.Stderr,
			"worldbit: --hash-every forces a single worker so the trace stays ordered")
	}
	fmt.Printf("worldbit %s: %d cells x %d seeds = %d runs, ~%s estimated\n",
		version, len(options.Cells), options.Seeds, runner.TotalRuns(options),
		roundEstimate(runner.EstimatedDuration(options)))
}

// roundEstimate trims an estimate to a legible precision. A minute-plus figure
// rounded to the second reads well; a sub-minute one rounded the same way turns
// a real few hundred milliseconds into a misleading "0s".
func roundEstimate(estimate time.Duration) time.Duration {
	if estimate >= time.Minute {
		return estimate.Round(time.Second)
	}
	return estimate.Round(100 * time.Millisecond)
}
