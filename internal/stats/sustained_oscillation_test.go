package stats

import "testing"

// The sustained-oscillation rule. See ClassifierConfig.MinOscillatingWindows
// for the derivation of the default.
//
// The rule exists because the high-variation flag used to LATCH: one window
// clearing the predicate settled the verdict for the whole run, so a single
// transient excursion was reported as sustained oscillation. The tests below
// pin both halves of that: one firing window must no longer be enough, and a
// run that fires repeatedly must still be OSCILLATING.

// quietWindowSeries neither fires the high-variation predicate nor counts as
// stable. Alternating 850/1150 gives a mean of 1000 and a coefficient of
// variation of 0.15 — above the 0.10 stable line and below the 0.25 high line —
// so it is the neutral filler these tests need: it advances the window boundary
// without firing the flag and without building a stable streak that would
// resolve the run early and skip the fallback entirely.
func quietWindowSeries(ticks int) []int { return swingSeries(850, 1150, ticks) }

// firingWindowSeries clears both gates on the flag. Alternating 400/1600 gives
// a coefficient of variation of 0.6 at a mean of 1000, so it is far past the
// 0.25 line and far past the 64-agent population floor.
func firingWindowSeries(ticks int) []int { return swingSeries(400, 1600, ticks) }

// windowedSeries builds a run out of one series per window, so a test can state
// exactly which windows fire. Window 0 is the burn-in window at the default
// configuration and is filler in every case below.
func windowedSeries(params ClassifierConfig, firing ...bool) []int {
	parts := make([][]int, len(firing))
	for index, fires := range firing {
		if fires {
			parts[index] = firingWindowSeries(params.WindowTicks)
		} else {
			parts[index] = quietWindowSeries(params.WindowTicks)
		}
	}
	return concatSeries(parts...)
}

// TestSustainedOscillationRejectsASingleWindow is the defect the rule exists to
// remove: one firing window in an otherwise unremarkable run used to latch the
// flag and resolve the whole run as OSCILLATING.
//
// With the default of two the run falls through the existing fallback order to
// TIMEOUT, which is the honest description of a run that had one excursion and
// otherwise did nothing worth naming.
func TestSustainedOscillationRejectsASingleWindow(t *testing.T) {
	params := DefaultClassifierConfig()
	samples := windowedSeries(params, false, true, false, false, false, false, false, false, false, false)

	latching := params
	latching.MinOscillatingWindows = 1
	if got, want := classifySeries(latching, samples).Outcome(), OutcomeOscillating; got != want {
		t.Fatalf("with the latching rule the series is %s, want %s — if this changed, the series "+
			"no longer demonstrates what the sustained rule fixes", got, want)
	}

	classifier := classifySeries(params, samples)
	if got, want := classifier.Outcome(), OutcomeTimeout; got != want {
		t.Errorf("outcome %s, want %s — one window still decided the run", got, want)
	}
	if got := classifier.HighVariationWindows(); got != 1 {
		t.Errorf("counted %d firing windows, want 1", got)
	}
}

// TestSustainedOscillationAcceptsTwoWindows is the other half: the rule must
// not have turned OSCILLATING off. Two firing windows is the default threshold,
// so this is the boundary case immediately above the one rejected.
func TestSustainedOscillationAcceptsTwoWindows(t *testing.T) {
	params := DefaultClassifierConfig()
	samples := windowedSeries(params, false, true, true, false, false, false, false, false, false, false)

	classifier := classifySeries(params, samples)
	if got, want := classifier.Outcome(), OutcomeOscillating; got != want {
		t.Errorf("outcome %s, want %s — two firing windows must reach the threshold", got, want)
	}
	if got := classifier.HighVariationWindows(); got != 2 {
		t.Errorf("counted %d firing windows, want 2", got)
	}
}

// TestSustainedOscillationCountsNonConsecutiveWindows is the one place this rule
// deliberately departs from the stable streak it otherwise mirrors, so it is
// pinned rather than left to be rediscovered.
//
// The count is TOTAL firing windows, not a consecutive run of them. Two windows
// separated by six quiet ones must resolve OSCILLATING exactly as two adjacent
// ones do. A consecutive rule would reject this, and it would reject real
// cycles: a cycle whose mean sits near MinOscillatingPopulation fails the
// population floor in its trough windows, so every trough would reset the
// streak and the cycle could never earn a verdict. See
// ClassifierConfig.MinOscillatingWindows.
func TestSustainedOscillationCountsNonConsecutiveWindows(t *testing.T) {
	params := DefaultClassifierConfig()
	samples := windowedSeries(params, false, true, false, false, false, false, false, false, true, false)

	classifier := classifySeries(params, samples)
	if got, want := classifier.Outcome(), OutcomeOscillating; got != want {
		t.Errorf("outcome %s, want %s — the firing windows must not have to be consecutive", got, want)
	}
	if got := classifier.HighVariationWindows(); got != 2 {
		t.Errorf("counted %d firing windows, want 2", got)
	}
}

// TestSustainedOscillationOneIsAnExactOptOut is the opt-out discipline every
// classification knob in this package follows: the disabling value must
// reproduce the previous behaviour exactly, not approximately.
//
// One is the opt-out here rather than zero, because the test is "at least N
// windows fired" and one is what makes that "at least one fired" — the latch.
func TestSustainedOscillationOneIsAnExactOptOut(t *testing.T) {
	params := DefaultClassifierConfig()
	params.MinOscillatingWindows = 1

	cases := []struct {
		name   string
		firing []bool
		want   Outcome
	}{
		{
			name:   "a single firing window latches, as it always did",
			firing: []bool{false, true, false, false, false, false, false, false, false, false},
			want:   OutcomeOscillating,
		},
		{
			name:   "a firing window inside burn-in still counts for nothing",
			firing: []bool{true, false, false, false, false, false, false, false, false, false},
			want:   OutcomeTimeout,
		},
		{
			name:   "no firing window is still not oscillating",
			firing: []bool{false, false, false, false, false, false, false, false, false, false},
			want:   OutcomeTimeout,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := classifySeries(params, windowedSeries(params, testCase.firing...)).Outcome()
			if got != testCase.want {
				t.Errorf("outcome %s, want %s", got, testCase.want)
			}
		})
	}
}

// TestSustainedOscillationThresholdIsRespectedAboveTheDefault checks the knob is
// a threshold rather than a hard-coded two, by walking a fixed three-firing-window
// series past it.
func TestSustainedOscillationThresholdIsRespectedAboveTheDefault(t *testing.T) {
	base := DefaultClassifierConfig()
	samples := windowedSeries(base, false, true, false, true, false, true, false, false, false, false)

	for threshold, want := range map[int]Outcome{
		1: OutcomeOscillating,
		2: OutcomeOscillating,
		3: OutcomeOscillating,
		4: OutcomeTimeout,
		9: OutcomeTimeout,
	} {
		params := base
		params.MinOscillatingWindows = threshold
		if got := classifySeries(params, samples).Outcome(); got != want {
			t.Errorf("MinOscillatingWindows=%d gives %s, want %s — three windows fired", threshold, got, want)
		}
	}
}

// TestSustainedOscillationComposesWithThePopulationFloor pins that the two gates
// on the flag are ANDed per window rather than applied to different tallies.
//
// The series fires the bare variation predicate in three windows, but two of
// them are a starving remnant below MinOscillatingPopulation. Only one window
// therefore counts, and the run must fall short of the two-window threshold —
// not be rescued by the two windows the population floor already rejected.
func TestSustainedOscillationComposesWithThePopulationFloor(t *testing.T) {
	params := DefaultClassifierConfig()
	window := params.WindowTicks

	// 8/24 has a coefficient of variation of 0.5, the same order as the
	// 400/1600 window, but a mean of 16 — far below the 64-agent floor.
	samples := concatSeries(
		quietWindowSeries(window),
		firingWindowSeries(window),
		swingSeries(8, 24, window),
		swingSeries(8, 24, window),
		quietWindowSeries(window),
	)

	classifier := classifySeries(params, samples)
	if got := classifier.HighVariationWindows(); got != 1 {
		t.Errorf("counted %d firing windows, want 1 — windows below the population floor must "+
			"not count towards the sustained threshold", got)
	}
	if got, want := classifier.Outcome(), OutcomeTimeout; got != want {
		t.Errorf("outcome %s, want %s — one qualifying window must not reach the threshold of %d",
			got, want, params.MinOscillatingWindows)
	}
}

// TestSustainedOscillationSurvivesAGenuineCycle is the regression guard on the
// thing the rule must never break: a large-amplitude cycle at a high population
// fires in every post-burn-in window and must stay OSCILLATING at any threshold
// the window count can reach.
func TestSustainedOscillationSurvivesAGenuineCycle(t *testing.T) {
	params := DefaultClassifierConfig()
	classifier := classifySeries(params, swingSeries(400, 1600, runTicks))

	if got, want := classifier.Outcome(), OutcomeOscillating; got != want {
		t.Errorf("outcome %s, want %s", got, want)
	}
	if got, want := classifier.HighVariationWindows(), 9; got != want {
		t.Errorf("counted %d firing windows, want %d — every window past the burn-in one",
			got, want)
	}
}

// TestNewClassifierRejectsNonPositiveOscillatingWindows keeps the one value that
// would invert the rule out of a configuration.
//
// Zero is not an opt-out here, unlike BurnInWindows and
// MinOscillatingPopulation: it would satisfy "at least zero windows fired" for
// every run and make OSCILLATING the universal fallback.
func TestNewClassifierRejectsNonPositiveOscillatingWindows(t *testing.T) {
	for _, threshold := range []int{0, -1} {
		func() {
			params := DefaultClassifierConfig()
			params.MinOscillatingWindows = threshold

			defer func() {
				if recover() == nil {
					t.Errorf("MinOscillatingWindows=%d was accepted", threshold)
				}
			}()
			NewClassifier(params)
		}()
	}
}
