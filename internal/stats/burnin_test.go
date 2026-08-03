package stats

import "testing"

// boomSeries is a caricature of how every run of this simulator opens: a small
// founding population, an enormous transient boom fed by the initial pantry,
// then a crash back to something sustainable. Nothing about the rest of the run
// can rescue this window's coefficient of variation.
func boomSeries(ticks int) []int {
	return seriesOf(ticks, func(index int) int {
		switch {
		case index < ticks/3:
			return 50
		case index < 2*ticks/3:
			return 2600
		default:
			return 700
		}
	})
}

// gentleDeclineSeries loses over 10 % of its own mean across the window while
// staying well inside the 0.25 coefficient-of-variation line, so it trips the
// declining predicate and nothing else. A steeper ramp would trip the
// high-variation flag too, and OSCILLATING wins over DECLINING at the tick
// limit — which would make it useless for isolating the slope fallback.
func gentleDeclineSeries(ticks int) []int {
	return seriesOf(ticks, func(index int) int { return 800 - index/8 })
}

// TestBurnInLetsAWildOpeningSettle is the requirement in its literal form: a
// series that is violent during the burn-in window and flat afterwards must
// classify STABLE.
//
// This case passes with burn-in switched off as well, and deliberately so:
// STABLE is terminal and is reached at the third flat window, before the
// end-of-run fallback that the burn-in window's variation would have poisoned
// ever runs. Burn-in must not break the case that already worked. The case that
// did NOT work is TestBurnInStopsOscillatingBeingTheDefaultVerdict.
func TestBurnInLetsAWildOpeningSettle(t *testing.T) {
	params := DefaultClassifierConfig()
	samples := concatSeries(
		boomSeries(params.WindowTicks),
		constantSeries(700, 5*params.WindowTicks),
	)

	classifier := classifySeries(params, samples)
	if got, want := classifier.Outcome(), OutcomeStable; got != want {
		t.Errorf("with one burn-in window the outcome is %s, want %s", got, want)
	}
	if want := 4 * params.WindowTicks; classifier.Tick() != want {
		t.Errorf("resolved at tick %d, want %d — three flat windows past the burn-in one",
			classifier.Tick(), want)
	}
}

// TestBurnInStopsOscillatingBeingTheDefaultVerdict is the failure the feature
// exists to remove.
//
// A run that settles but does not manage three consecutive stable windows
// before the tick limit falls through to the end-of-run fallback. Without
// burn-in the opening boom has already latched the high-variation flag, so that
// fallback is always OSCILLATING — the label then means "did not reach STABLE
// in time", not "genuinely oscillating", and it would be applied to whole
// regions of a parameter sweep that are nothing of the kind.
//
// The same series with one burn-in window falls through to TIMEOUT, which is
// the honest description of a run that settled too late to prove it.
//
// MinOscillatingWindows is held at its opt-out throughout, so that the only
// thing separating the two halves is burn-in. The sustained-oscillation rule
// would suppress this series on its own — the boom is a single window — and
// letting it do so would make this test pass for a reason that has nothing to
// do with what it is named after.
func TestBurnInStopsOscillatingBeingTheDefaultVerdict(t *testing.T) {
	params := DefaultClassifierConfig()
	params.MinOscillatingWindows = 1
	samples := concatSeries(
		boomSeries(params.WindowTicks),
		constantSeries(700, 2*params.WindowTicks),
	)

	noBurnIn := params
	noBurnIn.BurnInWindows = 0
	if got, want := classifySeries(noBurnIn, samples).Outcome(), OutcomeOscillating; got != want {
		t.Fatalf("without burn-in the series is %s, want %s — if this changed, the series no "+
			"longer demonstrates what burn-in fixes", got, want)
	}

	if got, want := classifySeries(params, samples).Outcome(), OutcomeTimeout; got != want {
		t.Errorf("with one burn-in window the outcome is %s, want %s — the opening window's "+
			"variation still decided the run", got, want)
	}
}

// TestBurnInExcludesEveryPredicate is the invariant that makes the feature
// coherent. A burn-in respected by some predicates and ignored by others would
// be worse than none, because the verdict would answer no single question.
//
// Each case isolates one predicate by making the burn-in window the ONLY window
// that could trip it.
func TestBurnInExcludesEveryPredicate(t *testing.T) {
	params := DefaultClassifierConfig()
	window := params.WindowTicks

	t.Run("the high-variation flag ignores burn-in", func(t *testing.T) {
		// Window 0 swings wildly; windows 1 and 2 are flat but there are only
		// two of them, so STABLE is out of reach and the fallback decides.
		// With the flag suppressed the fallback must be TIMEOUT, not
		// OSCILLATING.
		samples := concatSeries(
			seriesOf(window, func(index int) int {
				if index%2 == 0 {
					return 400
				}
				return 1600
			}),
			constantSeries(700, 2*window),
		)

		if got, want := classifySeries(params, samples).Outcome(), OutcomeTimeout; got != want {
			t.Errorf("outcome %s, want %s — the burn-in window's variation still latched the flag",
				got, want)
		}
	})

	t.Run("the stable streak ignores burn-in", func(t *testing.T) {
		// Three flat windows follow one crashing window. If the crash reset a
		// streak it was never part of, the run would need a fourth flat window
		// and would not resolve here at all.
		samples := concatSeries(
			seriesOf(window, func(index int) int { return 2600 - index*2 }),
			constantSeries(700, 3*window),
		)

		classifier := classifySeries(params, samples)
		if got, want := classifier.Outcome(), OutcomeStable; got != want {
			t.Fatalf("outcome %s, want %s", got, want)
		}
		if want := 4 * window; classifier.Tick() != want {
			t.Errorf("resolved at tick %d, want %d — the third window PAST burn-in",
				classifier.Tick(), want)
		}
	})

	t.Run("the declining fallback ignores burn-in", func(t *testing.T) {
		// The only declining window is the burn-in one, and it is also the
		// last: a run one window long. DECLINING must not be reachable from a
		// window no predicate was allowed to look at.
		samples := gentleDeclineSeries(window)

		noBurnIn := params
		noBurnIn.BurnInWindows = 0
		if got, want := classifySeries(noBurnIn, samples).Outcome(), OutcomeDeclining; got != want {
			t.Fatalf("without burn-in the series is %s, want %s — the series no longer "+
				"isolates the slope fallback", got, want)
		}

		if got, want := classifySeries(params, samples).Outcome(), OutcomeTimeout; got != want {
			t.Errorf("outcome %s, want %s — the burn-in window's slope still decided the run",
				got, want)
		}
	})
}

// TestBurnInZeroIsExactlyTheOldBehaviour proves the change is opt-out: with
// BurnInWindows at zero every series must classify, and resolve at the same
// tick, exactly as it did before burn-in existed.
//
// The expectations here are hard-coded rather than derived, so that a future
// change to the burn-in machinery that quietly altered the zero case would
// fail rather than move the goalposts with itself.
//
// "Before burn-in existed" also means before the sustained-oscillation rule
// existed, so MinOscillatingWindows is held at its own opt-out. Otherwise the
// boom case below would resolve TIMEOUT for a second, unrelated reason and this
// test would no longer be about BurnInWindows=0 at all.
func TestBurnInZeroIsExactlyTheOldBehaviour(t *testing.T) {
	params := DefaultClassifierConfig()
	params.BurnInWindows = 0
	params.MinOscillatingWindows = 1
	window := params.WindowTicks

	cases := []struct {
		name     string
		samples  []int
		want     Outcome
		wantTick int
	}{
		{
			name:     "flat run resolves at the third window",
			samples:  constantSeries(800, runTicks),
			want:     OutcomeStable,
			wantTick: 3 * window,
		},
		{
			// The settled tail is two windows long, one short of STABLE, so
			// the run falls through to the fallback the opening boom poisoned.
			name: "an opening boom makes an unsettled run oscillating",
			samples: concatSeries(
				boomSeries(window),
				constantSeries(700, 2*window),
			),
			want:     OutcomeOscillating,
			wantTick: 3 * window,
		},
		{
			name:     "a single declining window is DECLINING",
			samples:  gentleDeclineSeries(window),
			want:     OutcomeDeclining,
			wantTick: window,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			classifier := classifySeries(params, testCase.samples)
			if got := classifier.Outcome(); got != testCase.want {
				t.Errorf("outcome %s, want %s", got, testCase.want)
			}
			if classifier.Tick() != testCase.wantTick {
				t.Errorf("resolved at tick %d, want %d", classifier.Tick(), testCase.wantTick)
			}
		})
	}
}

// TestBurnInLongerThanTheRun covers the degenerate configuration: a burn-in
// that outlives the run must not panic, and must resolve as TIMEOUT, because no
// predicate was ever allowed to look at anything.
func TestBurnInLongerThanTheRun(t *testing.T) {
	params := DefaultClassifierConfig()
	params.BurnInWindows = 100

	classifier := classifySeries(params, constantSeries(800, 3*params.WindowTicks))

	if got, want := classifier.Outcome(), OutcomeTimeout; got != want {
		t.Errorf("outcome %s, want %s", got, want)
	}
	if classifier.WindowsClosed() != 3 {
		t.Errorf("closed %d windows, want 3 — burn-in must still advance the boundary",
			classifier.WindowsClosed())
	}
	if classifier.StableStreak() != 0 {
		t.Errorf("stable streak %d, want 0", classifier.StableStreak())
	}
	if classifier.PeakPopulation() != 800 {
		t.Errorf("peak %d, want 800 — burn-in must not suppress peak tracking",
			classifier.PeakPopulation())
	}
}

// TestBurnInDoesNotSuppressTerminalTicks pins the boundary of the feature.
// Burn-in excludes WINDOW predicates; extinction and overrun are per-tick and
// must still fire during it, or a run could die inside its burn-in and be
// reported as an uneventful timeout.
func TestBurnInDoesNotSuppressTerminalTicks(t *testing.T) {
	params := DefaultClassifierConfig()
	params.BurnInWindows = 10

	extinct := classifySeries(params, concatSeries(constantSeries(300, 50), constantSeries(0, 10)))
	if got, want := extinct.Outcome(), OutcomeExtinct; got != want {
		t.Errorf("outcome %s, want %s — extinction inside burn-in must still be terminal", got, want)
	}

	overrun := classifySeries(params, constantSeries(params.OverrunPopulation+1, 50))
	if got, want := overrun.Outcome(), OutcomeOverrun; got != want {
		t.Errorf("outcome %s, want %s — overrun inside burn-in must still be terminal", got, want)
	}
}

// TestNewClassifierRejectsNegativeBurnIn keeps a nonsensical configuration from
// silently classifying against thresholds nobody chose.
func TestNewClassifierRejectsNegativeBurnIn(t *testing.T) {
	params := DefaultClassifierConfig()
	params.BurnInWindows = -1

	defer func() {
		if recover() == nil {
			t.Error("a negative BurnInWindows was accepted")
		}
	}()
	NewClassifier(params)
}
