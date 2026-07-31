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

	"github.com/urfave/cli/v2"

	"github.com/vukyn/worldbit/internal/config"
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

// runHeadless is the batch entry point. The outcome classifier and the
// CSV/JSON writers arrive in later phases; for now it runs a single world and
// reports its canonical hash, which is what the determinism harness is for.
func runHeadless(c *cli.Context, cfg sim.Config) error {
	seed := c.Uint64("seed")
	hashEvery := int32(c.Int("hash-every"))

	if c.IsSet("runs") || c.IsSet("out") || c.IsSet("workers") {
		fmt.Fprintln(os.Stderr,
			"worldbit: --runs/--out/--workers are accepted but not wired yet; the batch runner "+
				"lands with the outcome classifier. Running a single seed.")
	}

	world := sim.NewWorld(seed, cfg)
	fmt.Printf("seed=%d ticks=%d config_hash=%016x version=%s\n", seed, cfg.MaxTick, cfg.Hash(), version)

	for world.Tick < cfg.MaxTick {
		sim.Step(world)
		if hashEvery > 0 && world.Tick%hashEvery == 0 {
			fmt.Printf("tick=%d hash=%016x\n", world.Tick, sim.Hash(world))
		}
	}

	fmt.Printf("tick=%d pop=%d state_hash=%016x\n", world.Tick, world.Population(), sim.Hash(world))
	return nil
}
