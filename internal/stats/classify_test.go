package stats

import "testing"

// seriesOf builds a population series from a shape function of the sample
// index. Index 0 is tick 1, matching Classifier.Observe.
func seriesOf(count int, shape func(index int) int) []int {
	samples := make([]int, count)
	for index := range samples {
		samples[index] = shape(index)
	}
	return samples
}

func constantSeries(value, count int) []int {
	return seriesOf(count, func(int) int { return value })
}

// concatSeries joins series head to tail, for runs that change character
// partway through.
func concatSeries(parts ...[]int) []int {
	joined := make([]int, 0)
	for _, part := range parts {
		joined = append(joined, part...)
	}
	return joined
}

// classifySeries feeds a whole series to a fresh classifier the way a run
// would: one Observe per tick, stopping at a terminal outcome, then Finish.
func classifySeries(params ClassifierConfig, samples []int) *Classifier {
	classifier := NewClassifier(params)
	for _, sample := range samples {
		classifier.Observe(sample)
		if classifier.Done() {
			break
		}
	}
	classifier.Finish()
	return classifier
}

const runTicks = 12000

// TestClassifyKnownSeries drives one hand-built series per outcome through the
// real state machine.
//
// Each series is chosen so that exactly one outcome is defensible, and the
// comment on each says which property of the series forces it. The value of
// this test is not that the arithmetic is right — the rational reference test
// covers that — but that the RESOLUTION ORDER is right: several of these
// series satisfy more than one description, and the classifier must pick the
// same one every time.
func TestClassifyKnownSeries(t *testing.T) {
	params := DefaultClassifierConfig()

	cases := []struct {
		name    string
		want    Outcome
		samples []int
		check   func(t *testing.T, classifier *Classifier)
	}{
		{
			// Zero population is terminal on the tick it happens, regardless
			// of what the window was doing.
			name: "extinct",
			want: OutcomeExtinct,
			samples: concatSeries(
				constantSeries(300, 500),
				constantSeries(0, 100),
			),
			check: func(t *testing.T, classifier *Classifier) {
				if classifier.Tick() != 501 {
					t.Errorf("stopped at tick %d, want 501 — extinction is terminal on the tick it happens",
						classifier.Tick())
				}
				if classifier.ExtinctYear() != 501/params.TicksPerYear {
					t.Errorf("extinct year %d, want %d", classifier.ExtinctYear(), 501/params.TicksPerYear)
				}
				if classifier.FinalPopulation() != 0 {
					t.Errorf("final population %d, want 0", classifier.FinalPopulation())
				}
				if classifier.PeakPopulation() != 300 || classifier.PeakTick() != 1 {
					t.Errorf("peak %d at tick %d, want 300 at tick 1",
						classifier.PeakPopulation(), classifier.PeakTick())
				}
			},
		},
		{
			// Flat and unvarying: three consecutive windows resolve it, so the
			// run ends at tick 3600 rather than at the tick limit.
			name:    "stable",
			want:    OutcomeStable,
			samples: constantSeries(800, runTicks),
			check: func(t *testing.T, classifier *Classifier) {
				if classifier.Tick() != 3*params.WindowTicks {
					t.Errorf("resolved at tick %d, want %d — STABLE is terminal at the third window",
						classifier.Tick(), 3*params.WindowTicks)
				}
				if classifier.WindowsEvaluated() != 3 {
					t.Errorf("evaluated %d windows, want 3", classifier.WindowsEvaluated())
				}
				if classifier.ExtinctYear() != -1 {
					t.Errorf("extinct year %d, want -1 for a run that never died", classifier.ExtinctYear())
				}
			},
		},
		{
			// Swings of ±60 % of the mean: every window is far past the 0.25
			// coefficient-of-variation line, and the trend is flat, so nothing
			// else fits.
			name: "oscillating",
			want: OutcomeOscillating,
			samples: seriesOf(runTicks, func(index int) int {
				if index%2 == 0 {
					return 400
				}
				return 1600
			}),
			check: func(t *testing.T, classifier *Classifier) {
				if classifier.Tick() != runTicks {
					t.Errorf("ran %d ticks, want the full %d — oscillation is only resolved at the limit",
						classifier.Tick(), runTicks)
				}
				if classifier.WindowsEvaluated() != runTicks/params.WindowTicks {
					t.Errorf("evaluated %d windows, want %d",
						classifier.WindowsEvaluated(), runTicks/params.WindowTicks)
				}
			},
		},
		{
			// Crosses the 6553 threshold at tick 101. Terminal, so the run
			// stops there instead of paying for the ticks after it.
			name: "overrun",
			want: OutcomeOverrun,
			samples: concatSeries(
				constantSeries(6000, 100),
				constantSeries(6554, 100),
			),
			check: func(t *testing.T, classifier *Classifier) {
				if classifier.Tick() != 101 {
					t.Errorf("stopped at tick %d, want 101", classifier.Tick())
				}
				if classifier.FinalPopulation() != 6554 {
					t.Errorf("final population %d, want 6554", classifier.FinalPopulation())
				}
				// Documented consequence of checking terminal outcomes before
				// tracking the peak: the population that tripped the threshold
				// is reported as the final population, not as the peak.
				if classifier.PeakPopulation() != 6000 {
					t.Errorf("peak population %d, want 6000 — the terminal tick is not a peak",
						classifier.PeakPopulation())
				}
			},
		},
		{
			// Loses 150 population per window on a mean that ends near 575:
			// −26 % per window in the final window, well past the −10 % line,
			// while the coefficient of variation stays around 0.08 so
			// oscillation never claims it first.
			name:    "declining",
			want:    OutcomeDeclining,
			samples: seriesOf(runTicks, func(index int) int { return 2000 - index/8 }),
			check: func(t *testing.T, classifier *Classifier) {
				if classifier.PeakPopulation() != 2000 {
					t.Errorf("peak population %d, want 2000", classifier.PeakPopulation())
				}
				if classifier.FinalPopulation() != 2000-(runTicks-1)/8 {
					t.Errorf("final population %d, want %d",
						classifier.FinalPopulation(), 2000-(runTicks-1)/8)
				}
			},
		},
		{
			// The mirror image of the declining series: a steady rise is too
			// steep to be flat, too smooth to oscillate, and the wrong sign to
			// be a decline. Nothing describes it, which is what TIMEOUT means.
			name:    "timeout",
			want:    OutcomeTimeout,
			samples: seriesOf(runTicks, func(index int) int { return 500 + index/8 }),
			check: func(t *testing.T, classifier *Classifier) {
				if classifier.StableStreak() != 0 {
					t.Errorf("stable streak %d, want 0", classifier.StableStreak())
				}
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			classifier := classifySeries(params, testCase.samples)

			if got := classifier.Outcome(); got != testCase.want {
				t.Fatalf("outcome %s, want %s (tick %d, pop %d, peak %d, windows %d)",
					got, testCase.want, classifier.Tick(), classifier.FinalPopulation(),
					classifier.PeakPopulation(), classifier.WindowsEvaluated())
			}
			if testCase.check != nil {
				testCase.check(t, classifier)
			}
		})
	}
}

// TestStableStreakMustBeConsecutive is the boundary case the "three windows"
// rule exists for: two stable windows are not three, and a run that
// destabilises after two must not be rescued by two more stable windows later.
func TestStableStreakMustBeConsecutive(t *testing.T) {
	params := DefaultClassifierConfig()

	samples := concatSeries(
		// Two perfectly stable windows: streak reaches 2.
		constantSeries(800, 2*params.WindowTicks),
		// Then violent swings for the rest of the run.
		seriesOf(runTicks-2*params.WindowTicks, func(index int) int {
			if index%2 == 0 {
				return 400
			}
			return 1600
		}),
	)

	classifier := classifySeries(params, samples)

	if classifier.Outcome() == OutcomeStable {
		t.Fatal("classified STABLE after only two consecutive stable windows")
	}
	if got, want := classifier.Outcome(), OutcomeOscillating; got != want {
		t.Errorf("outcome %s, want %s", got, want)
	}
	if classifier.StableStreak() != 0 {
		t.Errorf("stable streak %d at the tick limit, want 0 — the streak must reset on the first unstable window",
			classifier.StableStreak())
	}
	if classifier.Tick() != runTicks {
		t.Errorf("ran %d ticks, want the full %d", classifier.Tick(), runTicks)
	}
}

// TestStabilityFloorIsInclusive pins the 20-agent floor. Twenty agents holding
// perfectly steady is stable; nineteen is not, however steady it is, because a
// handful of survivors coasting is not an ecology.
func TestStabilityFloorIsInclusive(t *testing.T) {
	params := DefaultClassifierConfig()

	atFloor := classifySeries(params, constantSeries(params.MinStablePopulation, runTicks))
	if got := atFloor.Outcome(); got != OutcomeStable {
		t.Errorf("population exactly at the floor (%d) classified %s, want STABLE",
			params.MinStablePopulation, got)
	}

	belowFloor := classifySeries(params, constantSeries(params.MinStablePopulation-1, runTicks))
	if got := belowFloor.Outcome(); got == OutcomeStable {
		t.Errorf("population below the floor (%d) classified STABLE",
			params.MinStablePopulation-1)
	}
	if got, want := belowFloor.Outcome(), OutcomeTimeout; got != want {
		t.Errorf("population below the floor classified %s, want %s", got, want)
	}
	if belowFloor.StableStreak() != 0 {
		t.Errorf("stable streak %d below the floor, want 0", belowFloor.StableStreak())
	}
}

// TestOverrunThresholdMustBeExceeded pins the strict comparison. Exactly 6553
// agents is not an overrun; one more is.
func TestOverrunThresholdMustBeExceeded(t *testing.T) {
	params := DefaultClassifierConfig()

	if params.OverrunPopulation != 6553 {
		t.Fatalf("default overrun threshold is %d, want 6553 (40 %% of a 128x128 grid)",
			params.OverrunPopulation)
	}

	atThreshold := classifySeries(params, constantSeries(params.OverrunPopulation, runTicks))
	if got := atThreshold.Outcome(); got == OutcomeOverrun {
		t.Errorf("population exactly at the threshold (%d) classified OVERRUN",
			params.OverrunPopulation)
	}
	// Held exactly at the threshold it is also perfectly flat, so the only
	// other thing it can be is stable.
	if got, want := atThreshold.Outcome(), OutcomeStable; got != want {
		t.Errorf("population held at the threshold classified %s, want %s", got, want)
	}

	aboveThreshold := classifySeries(params, constantSeries(params.OverrunPopulation+1, runTicks))
	if got, want := aboveThreshold.Outcome(), OutcomeOverrun; got != want {
		t.Errorf("population one above the threshold classified %s, want %s", got, want)
	}
	if aboveThreshold.Tick() != 1 {
		t.Errorf("overrun detected at tick %d, want tick 1", aboveThreshold.Tick())
	}
}

// TestPartialFinalWindowIsDiscarded covers a run that ends mid-window. The
// leftover samples are not a window: they hold fewer ticks than every other
// window, so their predicates are not comparable and must not be evaluated.
func TestPartialFinalWindowIsDiscarded(t *testing.T) {
	params := DefaultClassifierConfig()

	classifier := classifySeries(params, constantSeries(800, params.WindowTicks-1))

	if classifier.WindowsEvaluated() != 0 {
		t.Errorf("evaluated %d windows from %d ticks, want 0",
			classifier.WindowsEvaluated(), params.WindowTicks-1)
	}
	if got, want := classifier.Outcome(), OutcomeTimeout; got != want {
		t.Errorf("outcome %s, want %s", got, want)
	}
}

// TestTerminalOutcomeIgnoresLaterTicks proves a verdict cannot be revised. A
// caller that keeps simulating past Done — the GUI replaying a run, for
// instance — must not be able to turn an EXTINCT run into something else.
func TestTerminalOutcomeIgnoresLaterTicks(t *testing.T) {
	params := DefaultClassifierConfig()
	classifier := NewClassifier(params)

	for tick := 0; tick < 10; tick++ {
		classifier.Observe(300)
	}
	classifier.Observe(0)

	if got, want := classifier.Outcome(), OutcomeExtinct; got != want {
		t.Fatalf("outcome %s, want %s", got, want)
	}

	tickAtDeath := classifier.Tick()
	for tick := 0; tick < 5000; tick++ {
		classifier.Observe(900)
	}

	if got, want := classifier.Outcome(), OutcomeExtinct; got != want {
		t.Errorf("outcome changed to %s after the terminal tick, want %s", got, want)
	}
	if classifier.Tick() != tickAtDeath {
		t.Errorf("tick advanced to %d past the terminal tick %d", classifier.Tick(), tickAtDeath)
	}
	if classifier.FinalPopulation() != 0 {
		t.Errorf("final population %d, want 0", classifier.FinalPopulation())
	}
	if got, want := classifier.Finish(), OutcomeExtinct; got != want {
		t.Errorf("Finish returned %s, want the terminal outcome %s", got, want)
	}
}

// TestFinishIsIdempotent: the runner calls Finish once, but the GUI may ask
// repeatedly while a replay sits at the tick limit.
func TestFinishIsIdempotent(t *testing.T) {
	params := DefaultClassifierConfig()
	classifier := classifySeries(params, seriesOf(runTicks, func(index int) int { return 500 + index/8 }))

	first := classifier.Outcome()
	for call := 0; call < 3; call++ {
		if got := classifier.Finish(); got != first {
			t.Fatalf("Finish call %d returned %s, want %s", call+2, got, first)
		}
	}
}

// TestObserveRejectsImpossiblePopulation pins the defensive assertion, and
// specifically that it is checked BEFORE the overrun test. Without that
// ordering an impossible population would be quietly filed as an ordinary
// OVERRUN, and the accumulator bounds it violates would go unreported.
func TestObserveRejectsImpossiblePopulation(t *testing.T) {
	cases := []struct {
		name       string
		population int
	}{
		{"above MaxSample", MaxSample + 1},
		{"far above MaxSample", 1 << 20},
		{"negative", -1},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Observe(%d) did not panic", testCase.population)
				}
			}()
			NewClassifier(DefaultClassifierConfig()).Observe(testCase.population)
		})
	}
}

// TestNewClassifierRejectsBadParameters covers the configuration assertions.
// These values reach the classifier from validated simulation config, so an
// invalid one is a programming error — and classifying against thresholds
// nobody chose is the silent-divergence failure this project exists to avoid.
func TestNewClassifierRejectsBadParameters(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ClassifierConfig)
	}{
		{"zero ticks per year", func(p *ClassifierConfig) { p.TicksPerYear = 0 }},
		{"zero stable windows", func(p *ClassifierConfig) { p.StableWindows = 0 }},
		{"zero overrun population", func(p *ClassifierConfig) { p.OverrunPopulation = 0 }},
		{"window too small", func(p *ClassifierConfig) { p.WindowTicks = 1 }},
		{"window too large", func(p *ClassifierConfig) { p.WindowTicks = MaxWindowSize + 1 }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic, got none")
				}
			}()
			params := DefaultClassifierConfig()
			testCase.mutate(&params)
			NewClassifier(params)
		})
	}
}

// TestOverrunPopulationForMatchesGrid pins the 40 %-of-cells rule, including
// that it rounds down.
func TestOverrunPopulationForMatchesGrid(t *testing.T) {
	cases := []struct {
		cells int
		want  int
	}{
		{128 * 128, 6553},
		{64 * 64, 1638},
		{256 * 256, 26214},
		{10, 4},
		// Floored at 1: 40 % of a two-cell grid rounds to zero, and a zero
		// threshold would make every population an overrun.
		{2, 1},
		{1, 1},
	}

	for _, testCase := range cases {
		if got := OverrunPopulationFor(testCase.cells); got != testCase.want {
			t.Errorf("OverrunPopulationFor(%d) = %d, want %d", testCase.cells, got, testCase.want)
		}
	}
}

// TestOutcomeStrings pins the names that end up in the CSV. Renaming one
// silently invalidates every previously written result file.
func TestOutcomeStrings(t *testing.T) {
	cases := []struct {
		outcome Outcome
		want    string
	}{
		{OutcomeRunning, "RUNNING"},
		{OutcomeExtinct, "EXTINCT"},
		{OutcomeStable, "STABLE"},
		{OutcomeOscillating, "OSCILLATING"},
		{OutcomeOverrun, "OVERRUN"},
		{OutcomeDeclining, "DECLINING"},
		{OutcomeTimeout, "TIMEOUT"},
		{Outcome(99), "Outcome(99)"},
	}

	for _, testCase := range cases {
		if got := testCase.outcome.String(); got != testCase.want {
			t.Errorf("Outcome(%d).String() = %q, want %q", testCase.outcome, got, testCase.want)
		}
	}
}
