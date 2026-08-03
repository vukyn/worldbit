package runner

import (
	"testing"

	"github.com/vukyn/worldbit/internal/sim"
)

// The end-to-end half of the sustained-oscillation rule. The unit tests in
// internal/stats drive hand-built series; these drive the real simulation, on
// the cells of the FoodRegrowTicks x BurnPerTick grid where the measurement
// found the latch and where it found genuine cycles.
//
// See stats.ClassifierConfig.MinOscillatingWindows for the rule and the
// derivation of the default of two.

// sustainedConfig builds one cell of the grid at the RATIFIED defaults —
// unlike sweepConfig in variation_floor_test.go, which opts the sustained rule
// out so that the population floor can be tested on its own.
func sustainedConfig(regrow int32, burn int16) sim.Config {
	cfg := sim.DefaultConfig()
	cfg.FoodRegrowTicks = regrow
	cfg.BurnPerTick = burn
	return cfg
}

// latchedRuns are the measured runs that rested on exactly ONE firing window.
//
// Every one of them was OSCILLATING under the latching rule and must not be
// under the sustained one. The destination is recorded too, because the point
// of the change is that these runs fall through the EXISTING fallback order
// rather than into some new bucket: a run whose final window is still losing
// ground is DECLINING, and one that has simply gone quiet is TIMEOUT.
//
// The three cells are the ones the 1200-run grid showed to be latch artefacts.
// FoodRegrowTicks=600/BurnPerTick=4 and 200/5 are not cycles by any reading of
// their trajectory — smoothed, they make 0-4 large excursions across the whole
// run. 800/5 does cycle, but at a mean population of 38-41, well under the
// ratified 64-agent floor, so only a stray window ever qualified.
var latchedRuns = []struct {
	regrow int32
	burn   int16
	seed   uint64
	want   string
}{
	{regrow: 200, burn: 5, seed: 3, want: "TIMEOUT"},
	{regrow: 200, burn: 5, seed: 6, want: "TIMEOUT"},
	{regrow: 600, burn: 4, seed: 1, want: "DECLINING"},
	{regrow: 600, burn: 4, seed: 4, want: "TIMEOUT"},
	{regrow: 800, burn: 5, seed: 1, want: "TIMEOUT"},
	{regrow: 800, burn: 5, seed: 2, want: "TIMEOUT"},
}

// TestSustainedOscillationDropsSingleWindowRuns is the change the rule was built
// to make, measured end to end.
func TestSustainedOscillationDropsSingleWindowRuns(t *testing.T) {
	for _, testCase := range latchedRuns {
		cfg := sustainedConfig(testCase.regrow, testCase.burn)

		latching := cfg
		latching.MinOscillatingWindows = 1
		if got := classifyRun(t, latching, testCase.seed); got != "OSCILLATING" {
			t.Fatalf("FoodRegrowTicks=%d BurnPerTick=%d seed %d was %s under the latching rule, "+
				"want OSCILLATING — the cell no longer demonstrates the problem",
				testCase.regrow, testCase.burn, testCase.seed, got)
		}

		got := classifyRun(t, cfg, testCase.seed)
		if got == "OSCILLATING" {
			t.Errorf("FoodRegrowTicks=%d BurnPerTick=%d seed %d is still OSCILLATING on one window",
				testCase.regrow, testCase.burn, testCase.seed)
		}
		if got != testCase.want {
			t.Errorf("FoodRegrowTicks=%d BurnPerTick=%d seed %d fell through to %s, want %s — "+
				"reclassified runs must land in the existing fallback order",
				testCase.regrow, testCase.burn, testCase.seed, got, testCase.want)
		}
	}
}

// TestSustainedOscillationKeepsGenuineCycles is the guard on the other side.
//
// BurnPerTick=5 with FoodRegrowTicks 300-600 is the genuine-cycle region: the
// population swings around a stationary mean at the carrying capacity for the
// whole run. FoodRegrowTicks=400 fires in 7-9 of its 9 evaluable windows and
// 500 in 5-8, so the rule must be nowhere near them. A threshold that emptied
// this region would be too high whatever it did to the latch.
func TestSustainedOscillationKeepsGenuineCycles(t *testing.T) {
	for _, regrow := range []int32{400, 500} {
		cfg := sustainedConfig(regrow, 5)
		for seed := uint64(1); seed <= 4; seed++ {
			if got := classifyRun(t, cfg, seed); got != "OSCILLATING" {
				t.Errorf("FoodRegrowTicks=%d BurnPerTick=5 seed %d classified %s, want OSCILLATING "+
					"— the sustained rule suppressed a genuine cycle", regrow, seed, got)
			}
		}
	}
}

// TestSustainedOscillationLeavesTerminalOutcomesAlone pins the reach of the
// change: the rule is consulted only in Finish, so it cannot touch a run that
// resolved before the tick limit.
//
// FoodRegrowTicks=600/BurnPerTick=4 seed 6 is the case worth having — it HAS a
// firing window and still classifies STABLE, because STABLE is terminal and was
// reached before the fallback ever ran. Seed 3 of the same cell has none and is
// TIMEOUT either way; 800/5 seed 3 is EXTINCT.
func TestSustainedOscillationLeavesTerminalOutcomesAlone(t *testing.T) {
	cases := []struct {
		regrow int32
		burn   int16
		seed   uint64
		want   string
	}{
		{regrow: 600, burn: 4, seed: 6, want: "STABLE"},
		{regrow: 600, burn: 4, seed: 3, want: "TIMEOUT"},
		{regrow: 800, burn: 5, seed: 3, want: "EXTINCT"},
	}

	for _, testCase := range cases {
		cfg := sustainedConfig(testCase.regrow, testCase.burn)

		latching := cfg
		latching.MinOscillatingWindows = 1
		before := classifyRun(t, latching, testCase.seed)
		after := classifyRun(t, cfg, testCase.seed)

		if before != testCase.want || after != testCase.want {
			t.Errorf("FoodRegrowTicks=%d BurnPerTick=%d seed %d: %s under the latching rule and %s "+
				"under the sustained one, want %s for both",
				testCase.regrow, testCase.burn, testCase.seed, before, after, testCase.want)
		}
	}
}

// TestSustainedOscillationDoesNotDisturbTheDefaults records that the ratified
// defaults are unaffected: the eight golden seeds all resolve STABLE, which
// never consults the high-variation flag, so the rule is inert there — which is
// what makes it safe to turn on by default.
func TestSustainedOscillationDoesNotDisturbTheGoldenSeeds(t *testing.T) {
	cfg := sim.DefaultConfig()

	for seed := uint64(1); seed <= 8; seed++ {
		if got := classifyRun(t, cfg, seed); got != "STABLE" {
			t.Errorf("seed %d classified %s, want STABLE — the sustained rule moved a golden seed",
				seed, got)
		}
	}
}

// TestClassifierParamsCarryTheSustainedWindowRule pins the field across the
// sim/stats boundary, alongside the other classification parameters.
func TestClassifierParamsCarryTheSustainedWindowRule(t *testing.T) {
	cfg := sim.DefaultConfig()
	cfg.MinOscillatingWindows = 5

	if got := ClassifierParams(cfg).MinOscillatingWindows; got != 5 {
		t.Errorf("MinOscillatingWindows = %d, want 5", got)
	}
}
