package runner

import (
	"testing"

	"github.com/vukyn/worldbit/internal/sim"
)

// The end-to-end half of the high-variation population floor. The unit tests in
// internal/stats drive hand-built series; these drive the real simulation, which
// is the only place the floor can be shown to reproduce — or deliberately not
// reproduce — the classifications the harness produced before it existed.
//
// See stats.ClassifierConfig.MinOscillatingPopulation for what the floor is and
// how its default was derived.

// classifyRun simulates one seed of one config and returns the outcome.
func classifyRun(t *testing.T, cfg sim.Config, seed uint64) string {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("invalid config: %v", err)
	}
	record, err := runOne(Options{Version: "test"}, Cell{Config: cfg}, seed)
	if err != nil {
		t.Fatalf("seed %d: %v", seed, err)
	}
	return record.Outcome
}

// goldenSeedOutcomesBeforeTheFloor is what the eight golden seeds classified as
// on the commit before the floor was added, at the default parameters.
//
// Hard-coded rather than computed from a second run with the floor disabled: a
// derived expectation would move with the code it is meant to pin.
var goldenSeedOutcomesBeforeTheFloor = []struct {
	seed uint64
	want string
}{
	{seed: 1, want: "STABLE"},
	{seed: 2, want: "STABLE"},
	{seed: 3, want: "STABLE"},
	{seed: 4, want: "STABLE"},
	{seed: 5, want: "DECLINING"},
	{seed: 6, want: "STABLE"},
	{seed: 7, want: "STABLE"},
	{seed: 8, want: "STABLE"},
}

// sweepOutcomesBeforeTheFloor is what a FoodRegrowTicks x BurnPerTick grid
// classified as before the floor was added.
//
// The grid is deliberately chosen to straddle the change: regrow 400 / burn 5 is
// a genuine high-population cycle that must SURVIVE the floor, while regrow 6400
// / burn 1 is the starving remnant that must not. A grid containing only cells
// of one kind would pass the floor-at-zero test while proving very little.
var sweepOutcomesBeforeTheFloor = []struct {
	regrow int32
	burn   int16
	seed   uint64
	want   string
}{
	{regrow: 400, burn: 1, seed: 1, want: "STABLE"},
	{regrow: 400, burn: 1, seed: 2, want: "STABLE"},
	{regrow: 400, burn: 1, seed: 3, want: "TIMEOUT"},
	{regrow: 400, burn: 1, seed: 4, want: "STABLE"},
	{regrow: 400, burn: 3, seed: 1, want: "TIMEOUT"},
	{regrow: 400, burn: 3, seed: 2, want: "TIMEOUT"},
	{regrow: 400, burn: 3, seed: 3, want: "TIMEOUT"},
	{regrow: 400, burn: 3, seed: 4, want: "TIMEOUT"},
	{regrow: 400, burn: 5, seed: 1, want: "OSCILLATING"},
	{regrow: 400, burn: 5, seed: 2, want: "OSCILLATING"},
	{regrow: 400, burn: 5, seed: 3, want: "OSCILLATING"},
	{regrow: 400, burn: 5, seed: 4, want: "OSCILLATING"},
	{regrow: 1600, burn: 1, seed: 1, want: "TIMEOUT"},
	{regrow: 1600, burn: 1, seed: 2, want: "TIMEOUT"},
	{regrow: 1600, burn: 1, seed: 3, want: "TIMEOUT"},
	{regrow: 1600, burn: 1, seed: 4, want: "TIMEOUT"},
	{regrow: 1600, burn: 3, seed: 1, want: "DECLINING"},
	{regrow: 1600, burn: 3, seed: 2, want: "OSCILLATING"},
	{regrow: 1600, burn: 3, seed: 3, want: "TIMEOUT"},
	{regrow: 1600, burn: 3, seed: 4, want: "OSCILLATING"},
	{regrow: 1600, burn: 5, seed: 1, want: "EXTINCT"},
	{regrow: 1600, burn: 5, seed: 2, want: "EXTINCT"},
	{regrow: 1600, burn: 5, seed: 3, want: "EXTINCT"},
	{regrow: 1600, burn: 5, seed: 4, want: "EXTINCT"},
	{regrow: 6400, burn: 1, seed: 1, want: "OSCILLATING"},
	{regrow: 6400, burn: 1, seed: 2, want: "OSCILLATING"},
	{regrow: 6400, burn: 1, seed: 3, want: "OSCILLATING"},
	{regrow: 6400, burn: 1, seed: 4, want: "OSCILLATING"},
	{regrow: 6400, burn: 3, seed: 1, want: "EXTINCT"},
	{regrow: 6400, burn: 3, seed: 2, want: "OSCILLATING"},
	{regrow: 6400, burn: 3, seed: 3, want: "EXTINCT"},
	{regrow: 6400, burn: 3, seed: 4, want: "EXTINCT"},
	{regrow: 6400, burn: 5, seed: 1, want: "EXTINCT"},
	{regrow: 6400, burn: 5, seed: 2, want: "EXTINCT"},
	{regrow: 6400, burn: 5, seed: 3, want: "EXTINCT"},
	{regrow: 6400, burn: 5, seed: 4, want: "EXTINCT"},
}

// sweepConfig builds one cell of the grid above.
func sweepConfig(regrow int32, burn int16) sim.Config {
	cfg := sim.DefaultConfig()
	cfg.FoodRegrowTicks = regrow
	cfg.BurnPerTick = burn
	return cfg
}

// TestHighVariationFloorZeroReproducesTheGoldenSeeds is the opt-out guarantee on
// the real simulation: with MinOscillatingPopulation=0 the eight golden seeds
// must classify exactly as they did before the floor existed.
func TestHighVariationFloorZeroReproducesTheGoldenSeeds(t *testing.T) {
	cfg := sim.DefaultConfig()
	cfg.MinOscillatingPopulation = 0

	for _, testCase := range goldenSeedOutcomesBeforeTheFloor {
		if got := classifyRun(t, cfg, testCase.seed); got != testCase.want {
			t.Errorf("seed %d classified %s, want %s — MinOscillatingPopulation=0 must reproduce "+
				"the pre-floor behaviour exactly", testCase.seed, got, testCase.want)
		}
	}
}

// TestHighVariationFloorZeroReproducesASweepGrid is the same guarantee across a
// grid that actually contains the outcomes the floor moves.
func TestHighVariationFloorZeroReproducesASweepGrid(t *testing.T) {
	for _, testCase := range sweepOutcomesBeforeTheFloor {
		cfg := sweepConfig(testCase.regrow, testCase.burn)
		cfg.MinOscillatingPopulation = 0

		if got := classifyRun(t, cfg, testCase.seed); got != testCase.want {
			t.Errorf("FoodRegrowTicks=%d BurnPerTick=%d seed %d classified %s, want %s — "+
				"MinOscillatingPopulation=0 must reproduce the pre-floor behaviour exactly",
				testCase.regrow, testCase.burn, testCase.seed, got, testCase.want)
		}
	}
}

// TestHighVariationFloorDefaultDoesNotDisturbTheGoldenSeeds records that the
// ratified defaults are unaffected. They resolve STABLE or DECLINING, neither of
// which consults the high-variation flag, so the floor is inert there — which is
// what makes it safe to turn on by default.
func TestHighVariationFloorDefaultDoesNotDisturbTheGoldenSeeds(t *testing.T) {
	cfg := sim.DefaultConfig()

	for _, testCase := range goldenSeedOutcomesBeforeTheFloor {
		if got := classifyRun(t, cfg, testCase.seed); got != testCase.want {
			t.Errorf("seed %d classified %s, want %s — the default floor changed a golden seed",
				testCase.seed, got, testCase.want)
		}
	}
}

// TestHighVariationFloorReclassifiesStarvingRemnants is the change the floor was
// built to make, measured end to end on the cell P4 identified.
//
// FoodRegrowTicks=6400 at BurnPerTick=1 has a carrying capacity of about 26
// agents. Every seed was reported OSCILLATING; the population is in fact a
// remnant that boomed on the initial pantry, collapsed, and then wandered in the
// teens and twenties for the rest of the run. With the floor it falls through to
// TIMEOUT or DECLINING — never back to OSCILLATING.
func TestHighVariationFloorReclassifiesStarvingRemnants(t *testing.T) {
	cfg := sweepConfig(6400, 1)

	for seed := uint64(1); seed <= 4; seed++ {
		before := cfg
		before.MinOscillatingPopulation = 0
		if got := classifyRun(t, before, seed); got != "OSCILLATING" {
			t.Fatalf("seed %d was %s before the floor, want OSCILLATING — the cell no longer "+
				"demonstrates the problem", seed, got)
		}

		got := classifyRun(t, cfg, seed)
		if got == "OSCILLATING" {
			t.Errorf("seed %d is still OSCILLATING with the default floor", seed)
		}
		if got != "TIMEOUT" && got != "DECLINING" {
			t.Errorf("seed %d fell through to %s; the fallback order should reach only TIMEOUT "+
				"or DECLINING from here", seed, got)
		}
	}
}

// TestHighVariationFloorKeepsGenuineCycles is the other side of the same change:
// FoodRegrowTicks=400 at BurnPerTick=5 sustains a real cycle — the population
// swings roughly between 30 and 180 about a stationary mean near the carrying
// capacity of 82, window after window, for the whole run. That is what
// OSCILLATING is for, and the floor must not touch it.
//
// This is also the cell that shows P4's conclusion that no genuine oscillation
// exists to have been a gap in coverage rather than a fact about the ecology.
func TestHighVariationFloorKeepsGenuineCycles(t *testing.T) {
	cfg := sweepConfig(400, 5)

	for seed := uint64(1); seed <= 4; seed++ {
		if got := classifyRun(t, cfg, seed); got != "OSCILLATING" {
			t.Errorf("seed %d classified %s with the default floor, want OSCILLATING — the floor "+
				"suppressed a genuine large-amplitude cycle", seed, got)
		}
	}
}

// TestClassifierParamsCarryTheHighVariationFloor pins the field across the
// sim/stats boundary, alongside the other classification parameters.
func TestClassifierParamsCarryTheHighVariationFloor(t *testing.T) {
	cfg := sim.DefaultConfig()
	cfg.MinOscillatingPopulation = 37

	if got := ClassifierParams(cfg).MinOscillatingPopulation; got != 37 {
		t.Errorf("MinOscillatingPopulation = %d, want 37", got)
	}
}
