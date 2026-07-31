package stats

import "testing"

// The high-variation population floor. See
// ClassifierConfig.MinOscillatingPopulation for the derivation of the default.
//
// The floor exists because the coefficient of variation is scale-free: a
// population wandering between 10 and 40 clears the 0.25 line trivially, while
// one wandering between 700 and 900 does not, even though only the second is
// what "oscillating ecology" is meant to describe. The tests below pin both
// halves of that: a large-amplitude cycle at a high population must still be
// OSCILLATING, and a small noisy remnant must not.

// swingSeries alternates between two populations every tick. The coefficient of
// variation of such a window is |high-low| / (high+low), so the caller can put
// a series either side of the 0.25 line by choosing the pair.
func swingSeries(low, high, ticks int) []int {
	return seriesOf(ticks, func(index int) int {
		if index%2 == 0 {
			return low
		}
		return high
	})
}

// TestHighVariationFloorKeepsLargeAmplitudeOscillationAtHighPopulation is the
// half of the floor that must NOT change: a genuine cycle — large swings about
// a stationary mean, at a population where a 0.25 coefficient of variation is
// far beyond anything demographic noise could produce — is exactly what
// OSCILLATING is for, and the floor must leave it alone.
//
// Swinging between 400 and 1600 gives a mean of 1000 and a coefficient of
// variation of 0.6, so the window clears both the variation line and the
// default 64-agent floor by a wide margin.
func TestHighVariationFloorKeepsLargeAmplitudeOscillationAtHighPopulation(t *testing.T) {
	params := DefaultClassifierConfig()
	samples := swingSeries(400, 1600, runTicks)

	classifier := classifySeries(params, samples)
	if got, want := classifier.Outcome(), OutcomeOscillating; got != want {
		t.Errorf("outcome %s, want %s — the floor suppressed a genuine high-population cycle", got, want)
	}
	if classifier.Tick() != runTicks {
		t.Errorf("ran %d ticks, want the full %d", classifier.Tick(), runTicks)
	}
}

// TestHighVariationFloorRejectsSmallNoisyRemnant is the half the floor exists
// for, and the failure P4 measured: a starving remnant of a dozen-odd agents
// whose small-number noise trivially clears the coefficient-of-variation line.
//
// The series swings between 8 and 24 — a mean of 16 and a coefficient of
// variation of 0.5, so it fires the bare predicate just as hard as the 400/1600
// cycle above — but at that population the variation says nothing, because
// demographic noise alone produces a coefficient of variation near 0.25 there.
//
// The run must fall through the existing fallback order instead. Its trend is
// flat rather than declining and its population is below MinStablePopulation,
// so the honest description is TIMEOUT: not dying, not stable, not oscillating,
// just small.
func TestHighVariationFloorRejectsSmallNoisyRemnant(t *testing.T) {
	params := DefaultClassifierConfig()
	samples := swingSeries(8, 24, runTicks)

	unfloored := params
	unfloored.MinOscillatingPopulation = 0
	if got, want := classifySeries(unfloored, samples).Outcome(), OutcomeOscillating; got != want {
		t.Fatalf("without the floor the series is %s, want %s — if this changed, the series no "+
			"longer demonstrates what the floor fixes", got, want)
	}

	if got, want := classifySeries(params, samples).Outcome(), OutcomeTimeout; got != want {
		t.Errorf("with the floor the outcome is %s, want %s", got, want)
	}
}

// TestHighVariationFloorRemnantFallsThroughToDeclining pins the OTHER
// destination a rejected remnant can reach, so that the fallback order is shown
// to be doing the work rather than one branch of it.
//
// A small population that is both noisy and losing ground must land on
// DECLINING, because the declining-slope fallback sits directly behind the
// high-variation flag.
func TestHighVariationFloorRemnantFallsThroughToDeclining(t *testing.T) {
	params := DefaultClassifierConfig()

	// Noisy enough to fire the bare variation predicate in every window, and
	// losing more than 10 % of the window mean across the final window.
	samples := seriesOf(runTicks, func(index int) int {
		level := 40 - index/400
		if index%2 == 0 {
			return level / 2
		}
		return level * 3 / 2
	})

	unfloored := params
	unfloored.MinOscillatingPopulation = 0
	if got, want := classifySeries(unfloored, samples).Outcome(), OutcomeOscillating; got != want {
		t.Fatalf("without the floor the series is %s, want %s", got, want)
	}

	if got, want := classifySeries(params, samples).Outcome(), OutcomeDeclining; got != want {
		t.Errorf("with the floor the outcome is %s, want %s — a shrinking noisy remnant is a "+
			"decline, not a cycle", got, want)
	}
}

// TestHighVariationFloorBoundary pins the comparison itself at the boundary.
//
// MeanAtLeast is `S1 >= floor*n`, so a window whose mean is EXACTLY the floor
// qualifies and one a single agent below it does not. Off-by-one here would be
// invisible in every other test, because nothing else sits on the line.
func TestHighVariationFloorBoundary(t *testing.T) {
	const floor = 64

	// Both series fire HighVariation; they differ only in mean population.
	// Swinging low/high gives a mean of (low+high)/2 exactly, because the
	// window length is even.
	cases := []struct {
		name string
		low  int
		high int
		want Outcome
	}{
		// Mean exactly 64: qualifies, because the comparison is >=.
		{name: "mean exactly at the floor", low: 32, high: 96, want: OutcomeOscillating},
		// Mean exactly 63: one agent short, rejected.
		{name: "mean one below the floor", low: 31, high: 95, want: OutcomeTimeout},
		// Mean exactly 65: clear.
		{name: "mean one above the floor", low: 33, high: 97, want: OutcomeOscillating},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			params := DefaultClassifierConfig()
			params.MinOscillatingPopulation = floor
			samples := swingSeries(testCase.low, testCase.high, runTicks)

			// Guard the premise: every one of these must clear the bare
			// variation line, or the test would be measuring the wrong thing.
			window := NewWindow(params.WindowTicks)
			for index := 0; index < params.WindowTicks; index++ {
				window.Add(samples[index])
			}
			if !window.HighVariation() {
				t.Fatalf("series %d/%d does not fire HighVariation; the case tests nothing",
					testCase.low, testCase.high)
			}
			if mean := window.Sum() / int64(window.Count()); mean != int64((testCase.low+testCase.high)/2) {
				t.Fatalf("window mean %d, want %d", mean, (testCase.low+testCase.high)/2)
			}

			if got := classifySeries(params, samples).Outcome(); got != testCase.want {
				t.Errorf("outcome %s, want %s", got, testCase.want)
			}
		})
	}
}

// TestMeanAtLeastIsExactAtTheBoundary tests the predicate directly, including
// the cases the classifier cannot reach: a floor of zero, and a window whose
// mean is not an integer.
func TestMeanAtLeastIsExactAtTheBoundary(t *testing.T) {
	// Three samples summing to 100: mean 33.33, which no integer floor can
	// equal, so the rounding direction is observable.
	window := NewWindow(3)
	for _, sample := range []int{30, 30, 40} {
		window.Add(sample)
	}

	cases := []struct {
		floor int
		want  bool
	}{
		{floor: 0, want: true},
		{floor: 1, want: true},
		{floor: 33, want: true},  // 100 >= 99
		{floor: 34, want: false}, // 100 < 102 — no rounding up
	}
	for _, testCase := range cases {
		if got := window.MeanAtLeast(testCase.floor); got != testCase.want {
			t.Errorf("MeanAtLeast(%d) = %t, want %t (S1=%d n=%d)",
				testCase.floor, got, testCase.want, window.Sum(), window.Count())
		}
	}

	// An empty window has S1 = 0 and n = 0, so every floor is trivially
	// satisfied. That is harmless because HighVariation rejects it first, but
	// it must not panic or divide.
	empty := NewWindow(2)
	if !empty.MeanAtLeast(1000) {
		t.Error("MeanAtLeast on an empty window should be vacuously true, not false")
	}
}

// TestHighVariationFloorZeroIsExactlyTheOldBehaviour is the opt-out guarantee,
// the same discipline BurnInWindows=0 is held to: with the floor disabled, every
// series must classify exactly as it did before the floor existed.
//
// The expectations are hard-coded rather than derived from a second classifier
// run, so that a change to the floored path cannot silently move both sides of
// the comparison together.
func TestHighVariationFloorZeroIsExactlyTheOldBehaviour(t *testing.T) {
	params := DefaultClassifierConfig()
	params.MinOscillatingPopulation = 0
	window := params.WindowTicks

	cases := []struct {
		name    string
		samples []int
		want    Outcome
	}{
		{
			// A remnant an order of magnitude below the floor: the case the
			// floor was built to reject, which at zero must still be
			// OSCILLATING.
			name:    "tiny noisy remnant",
			samples: swingSeries(8, 24, runTicks),
			want:    OutcomeOscillating,
		},
		{
			name:    "large-amplitude cycle at high population",
			samples: swingSeries(400, 1600, runTicks),
			want:    OutcomeOscillating,
		},
		{
			name:    "flat and populous",
			samples: constantSeries(700, runTicks),
			want:    OutcomeStable,
		},
		{
			name:    "gentle decline",
			samples: concatSeries(gentleDeclineSeries(window), gentleDeclineSeries(window), gentleDeclineSeries(window)),
			want:    OutcomeDeclining,
		},
		{
			// Burn-in and the floor are independent: with the floor off, the
			// burn-in behaviour must be untouched.
			name:    "wild opening then flat, one burn-in window",
			samples: concatSeries(boomSeries(window), constantSeries(700, 2*window)),
			want:    OutcomeTimeout,
		},
		{
			name:    "extinction",
			samples: concatSeries(constantSeries(700, window), constantSeries(0, 1)),
			want:    OutcomeExtinct,
		},
		{
			name:    "overrun",
			samples: concatSeries(constantSeries(700, window), constantSeries(6554, 1)),
			want:    OutcomeOverrun,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := classifySeries(params, testCase.samples).Outcome(); got != testCase.want {
				t.Errorf("outcome %s, want %s — MinOscillatingPopulation=0 must reproduce the "+
					"un-floored behaviour exactly", got, testCase.want)
			}
		})
	}
}

// TestNegativeHighVariationFloorPanics matches the treatment of every other
// nonsensical threshold: these values come from validated config, so an invalid
// one is a programming error and must not be silently clamped.
func TestNegativeHighVariationFloorPanics(t *testing.T) {
	params := DefaultClassifierConfig()
	params.MinOscillatingPopulation = -1

	defer func() {
		if recover() == nil {
			t.Error("a negative MinOscillatingPopulation was accepted")
		}
	}()
	NewClassifier(params)
}
