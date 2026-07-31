package stats

import (
	"math/big"
	"math/rand/v2"
	"testing"
)

// referencePredicates evaluates the four spec predicates with exact rational
// arithmetic, as the specification states them rather than in the integer form
// the classifier uses:
//
//	stddev/mean < 0.10        stddev/mean > 0.25
//	|slope·n/mean| < 0.05     slope·n/mean < −0.10
//
// big.Rat is exact, so agreement with it is not "close enough" — it is
// equality, including at the thresholds. The square root is avoided by
// comparing the SQUARED coefficient of variation against the squared
// thresholds, which is equivalent because both sides are non-negative and is
// the same manipulation the integer forms are derived from.
//
// This reference is test-only. Production code may not allocate per tick, and
// may not depend on math/big.
func referencePredicates(samples []int) (stableVariation, highVariation, flatSlope, decliningSlope bool) {
	n := int64(len(samples))
	if n < 1 {
		return false, false, false, false
	}

	sumP := new(big.Int)
	sumPP := new(big.Int)
	sumIP := new(big.Int)
	for index, sample := range samples {
		population := big.NewInt(int64(sample))
		sumP.Add(sumP, population)
		sumPP.Add(sumPP, new(big.Int).Mul(population, population))
		sumIP.Add(sumIP, new(big.Int).Mul(big.NewInt(int64(index)), population))
	}

	// A window whose mean is zero has no coefficient of variation and no
	// relative slope: every predicate divides by the mean. The classifier
	// treats such a window as matching nothing.
	if sumP.Sign() <= 0 {
		return false, false, false, false
	}

	count := big.NewInt(n)
	mean := new(big.Rat).SetFrac(sumP, count)
	meanSquared := new(big.Rat).Mul(mean, mean)

	variance := new(big.Rat).Sub(new(big.Rat).SetFrac(sumPP, count), meanSquared)
	cvSquared := new(big.Rat).Quo(variance, meanSquared)

	stableVariation = cvSquared.Cmp(big.NewRat(1, 100)) < 0
	highVariation = cvSquared.Cmp(big.NewRat(1, 16)) > 0

	if n < 2 {
		return stableVariation, highVariation, false, false
	}

	sx := new(big.Int).Div(new(big.Int).Mul(count, big.NewInt(n-1)), big.NewInt(2))
	sxx := new(big.Int).Div(
		new(big.Int).Mul(new(big.Int).Mul(big.NewInt(n-1), count), big.NewInt(2*n-1)),
		big.NewInt(6),
	)

	numerator := new(big.Int).Sub(new(big.Int).Mul(count, sumIP), new(big.Int).Mul(sx, sumP))
	denominator := new(big.Int).Sub(new(big.Int).Mul(count, sxx), new(big.Int).Mul(sx, sx))

	slope := new(big.Rat).SetFrac(numerator, denominator)
	relative := new(big.Rat).Quo(new(big.Rat).Mul(slope, new(big.Rat).SetInt(count)), mean)

	magnitude := new(big.Rat).Abs(relative)
	flatSlope = magnitude.Cmp(big.NewRat(1, 20)) < 0
	decliningSlope = relative.Cmp(big.NewRat(-1, 10)) < 0

	return stableVariation, highVariation, flatSlope, decliningSlope
}

// windowOf fills a window with the given samples.
func windowOf(t *testing.T, samples []int) Window {
	t.Helper()

	window := NewWindow(len(samples))
	for _, sample := range samples {
		window.Add(sample)
	}
	return window
}

// checkAgainstReference asserts all four integer predicates agree exactly with
// the rational reference.
func checkAgainstReference(t *testing.T, label string, samples []int) {
	t.Helper()

	window := windowOf(t, samples)
	wantStable, wantHigh, wantFlat, wantDeclining := referencePredicates(samples)

	checks := []struct {
		name string
		got  bool
		want bool
	}{
		{"StableVariation", window.StableVariation(), wantStable},
		{"HighVariation", window.HighVariation(), wantHigh},
		{"FlatSlope", window.FlatSlope(), wantFlat},
		{"DecliningSlope", window.DecliningSlope(), wantDeclining},
	}

	for _, check := range checks {
		if check.got != check.want {
			t.Fatalf("%s: %s = %v, big.Rat reference says %v (n=%d S1=%d S2=%d Sxy=%d)",
				label, check.name, check.got, check.want,
				window.Count(), window.Sum(), window.SumOfSquares(), window.SumOfIndexProducts())
		}
	}
}

// TestClassifierPredicatesMatchRationalReference is the numerics gate: the
// integer predicates must agree with exact rational arithmetic on every window,
// not merely usually.
//
// Uniform random samples would make every window wildly variable and every
// predicate trivially false, so the generators below are built to land NEAR the
// thresholds — tight clusters around a base for the variation predicates,
// ramps of controlled relative gradient for the slope ones. A predicate that
// only disagrees within a hair of its threshold is exactly the bug this test
// is for.
func TestClassifierPredicatesMatchRationalReference(t *testing.T) {
	random := rand.New(rand.NewPCG(2024, 7))

	sizes := []int{2, 3, 5, 16, 60, 120, 1200}

	for _, size := range sizes {
		for round := 0; round < 400; round++ {
			base := 1 + random.IntN(3000)

			// Spread chosen so the coefficient of variation straddles both
			// 0.10 and 0.25 across rounds.
			spread := random.IntN(base) / 2

			// Gradient across the whole window as a permille of the base, from
			// -300 to +300, straddling both slope thresholds (±50 and -100).
			gradientPermille := random.IntN(601) - 300

			samples := make([]int, size)
			for index := range samples {
				trend := base * gradientPermille * index / (1000 * size)
				noise := 0
				if spread > 0 {
					noise = random.IntN(2*spread+1) - spread
				}

				sample := base + trend + noise
				if sample < 0 {
					sample = 0
				}
				if sample > MaxSample {
					sample = MaxSample
				}
				samples[index] = sample
			}

			checkAgainstReference(t, "randomised", samples)
		}
	}

	// Degenerate and extreme shapes the generator above will not produce.
	special := []struct {
		label   string
		samples []int
	}{
		{"constant", []int{800, 800, 800, 800, 800, 800}},
		{"all zero", []int{0, 0, 0, 0}},
		{"single zero sample", []int{0, 5, 0, 5}},
		{"one big spike", []int{10, 10, 10, 6553, 10, 10}},
		{"crash to one", []int{5000, 4000, 3000, 2000, 1000, 1}},
		{"maximum samples", []int{MaxSample, MaxSample, MaxSample, MaxSample}},
		{"maximum then floor", []int{MaxSample, MaxSample, 1, 1}},
		{"two identical", []int{42, 42}},
		{"two point rise", []int{1, 4096}},
		{"strict monotone dip", []int{100, 99, 98, 97, 96, 95, 94, 93}},
	}
	for _, testCase := range special {
		checkAgainstReference(t, testCase.label, testCase.samples)
	}
}

// TestWindowPredicatesAreStrictAtTheThreshold pins the comparison operators.
//
// Every predicate in the specification is strict, and each of these two-sample
// windows sits EXACTLY on one threshold — chosen by solving the predicate for
// integers rather than by searching, so they cannot drift. A `<=` slipped in
// anywhere flips exactly one of these.
func TestWindowPredicatesAreStrictAtTheThreshold(t *testing.T) {
	cases := []struct {
		name      string
		samples   []int
		predicate func(Window) bool
		exactly   string
	}{
		// (11-9)/(11+9) = 0.10 exactly.
		{"variation exactly 0.10", []int{11, 9}, Window.StableVariation, "cv = 0.10, and < 0.10 is strict"},
		// (5-3)/(5+3) = 0.25 exactly.
		{"variation exactly 0.25", []int{5, 3}, Window.HighVariation, "cv = 0.25, and > 0.25 is strict"},
		// 4·(81-79)/(81+79) = +0.05 exactly.
		{"relative slope exactly +0.05", []int{79, 81}, Window.FlatSlope, "slope·n/mean = +0.05"},
		{"relative slope exactly -0.05", []int{81, 79}, Window.FlatSlope, "slope·n/mean = -0.05"},
		// 4·(39-41)/(39+41) = -0.10 exactly.
		{"relative slope exactly -0.10", []int{41, 39}, Window.DecliningSlope, "slope·n/mean = -0.10"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			window := windowOf(t, testCase.samples)
			if testCase.predicate(window) {
				t.Errorf("predicate is true on %v but %s, so it must be false",
					testCase.samples, testCase.exactly)
			}
			// The rational reference must agree that this is the boundary.
			checkAgainstReference(t, testCase.name, testCase.samples)
		})
	}
}

// TestWindowZeroMeanMatchesNothing covers the window whose samples are all
// zero. Every predicate divides by the mean, so there is no honest answer;
// the classifier's contract is that such a window matches nothing.
//
// A live run cannot produce one — a population of zero is EXTINCT on the tick
// it happens, which is terminal — but Window is usable on its own and must not
// divide by zero or report a spurious "perfectly stable at zero".
func TestWindowZeroMeanMatchesNothing(t *testing.T) {
	window := NewWindow(1200)
	for tick := 0; tick < 1200; tick++ {
		window.Add(0)
	}

	if !window.Full() {
		t.Fatalf("window holds %d of %d samples", window.Count(), window.Size())
	}
	if window.StableVariation() || window.HighVariation() || window.FlatSlope() || window.DecliningSlope() {
		t.Errorf("a zero-mean window matched a predicate: stable=%v high=%v flat=%v declining=%v",
			window.StableVariation(), window.HighVariation(),
			window.FlatSlope(), window.DecliningSlope())
	}
}

// TestWindowSlopeDenominatorMatchesClosedForm checks D = n·Sxx − Sx² against
// n²(n²−1)/12 computed in arbitrary precision.
//
// The two are algebraically identical, which is the point: the accumulators
// compute D the long way, and this pins that the long way stays exact — an
// off-by-one in Sxx would otherwise skew every slope by a few permille and
// never trip a test that only checks the predicates' sign.
func TestWindowSlopeDenominatorMatchesClosedForm(t *testing.T) {
	for _, size := range []int{2, 3, 7, 120, 1200, MaxWindowSize} {
		window := NewWindow(size)
		for index := 0; index < size; index++ {
			window.Add(1)
		}

		n := big.NewInt(int64(size))
		nSquared := new(big.Int).Mul(n, n)
		closedForm := new(big.Int).Div(
			new(big.Int).Mul(nSquared, new(big.Int).Sub(nSquared, big.NewInt(1))),
			big.NewInt(12),
		)

		if got := big.NewInt(window.slopeDenominator()); got.Cmp(closedForm) != 0 {
			t.Errorf("n=%d: n·Sxx − Sx² = %s, closed form n²(n²−1)/12 = %s",
				size, got, closedForm)
		}
	}
}

// TestWindowRejectsOutOfRangeInputs pins the defensive assertions. They are
// panics rather than errors because every overflow bound in this package is
// stated in terms of them: a sample past MaxSample does not degrade the
// answer, it silently wraps an accumulator.
func TestWindowRejectsOutOfRangeInputs(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"window too small", func() { NewWindow(1) }},
		{"window too large", func() { NewWindow(MaxWindowSize + 1) }},
		{"sample above MaxSample", func() {
			window := NewWindow(2)
			window.Add(MaxSample + 1)
		}},
		{"negative sample", func() {
			window := NewWindow(2)
			window.Add(-1)
		}},
		{"add to a full window", func() {
			window := NewWindow(2)
			window.Add(1)
			window.Add(1)
			window.Add(1)
		}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic, got none")
				}
			}()
			testCase.call()
		})
	}

	// The bound itself must be accepted, not merely approached.
	window := NewWindow(2)
	window.Add(MaxSample)
	window.Add(MaxSample)
	if window.Sum() != 2*MaxSample {
		t.Errorf("S1 = %d, want %d", window.Sum(), 2*MaxSample)
	}
}

// TestWindowResetMakesWindowsNonOverlapping pins the property the whole
// "three consecutive windows" rule rests on: after Reset a window shares
// nothing with the one before it, including the sample index the slope is
// measured against.
func TestWindowResetMakesWindowsNonOverlapping(t *testing.T) {
	window := NewWindow(3)
	for _, sample := range []int{900, 800, 700} {
		window.Add(sample)
	}

	firstNumerator := window.slopeNumerator()
	window.Reset()

	if window.Count() != 0 || window.Sum() != 0 || window.SumOfSquares() != 0 || window.SumOfIndexProducts() != 0 {
		t.Fatalf("Reset left state behind: count=%d S1=%d S2=%d Sxy=%d",
			window.Count(), window.Sum(), window.SumOfSquares(), window.SumOfIndexProducts())
	}

	for _, sample := range []int{900, 800, 700} {
		window.Add(sample)
	}
	if second := window.slopeNumerator(); second != firstNumerator {
		t.Errorf("the same samples after Reset give slope numerator %d, want %d — "+
			"the index must restart at zero in every window", second, firstNumerator)
	}
}
