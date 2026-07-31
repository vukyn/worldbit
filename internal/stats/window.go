package stats

import "strconv"

const (
	// MaxSample is the largest population sample the accumulators accept.
	//
	// This is a defensive bound, not an expected value: every overflow
	// argument below is stated in terms of it, so a population that somehow
	// escapes the simulation's own limits must fail loudly here rather than
	// silently wrap an accumulator and produce a confidently wrong outcome.
	// The simulation's own ceiling is far lower — OVERRUN is terminal at 40 %
	// of the grid, 6553 agents on the default 128x128 board.
	MaxSample = 65535

	// MaxWindowSize is the largest window the int64 accumulators are proven
	// safe for, given MaxSample.
	//
	// The binding constraint is the variation predicate, whose largest term is
	// 100·n·S2 with S2 ≤ n·MaxSample²:
	//
	//	100 · 4096 · (4096 · 65535²) = 7.21e18 < 9.22e18 = MaxInt64
	//
	// and its right-hand side 101·S1² with S1 ≤ n·MaxSample:
	//
	//	101 · (4096 · 65535)²        = 7.28e18 < MaxInt64
	//
	// The slope terms are wider still (≈7.6e23) and are therefore compared as
	// exact 128-bit products; see FlatSlope and DecliningSlope. The default
	// window is 1200 ticks, so this ceiling is three and a half times the size
	// anything in the simulator actually asks for.
	MaxWindowSize = 4096
)

// Window accumulates the integer sums that the classifier's variation and
// slope predicates are built from, in O(1) per sample and constant space.
//
// Windows are NON-OVERLAPPING blocks: the classifier fills one, evaluates the
// predicates on it, and resets. That is the only reading of the specification
// under which "three consecutive windows" is meaningful, and it is what
// collapses the classifier to five int64 accumulators — a sliding window would
// need a ring buffer of every sample in flight.
//
// For n samples p_0 … p_{n-1}, indexed from zero within the window:
//
//	S1  = Σ p_i
//	S2  = Σ p_i²
//	Sxy = Σ i·p_i
//	Sx  = Σ i   = n(n-1)/2          (constant, depends only on n)
//	Sxx = Σ i²  = (n-1)n(2n-1)/6    (constant, depends only on n)
//
// The zero Window is not usable; construct one with NewWindow.
type Window struct {
	size  int64
	count int64
	sumP  int64 // S1
	sumPP int64 // S2
	sumIP int64 // Sxy
}

// NewWindow returns an empty window of the given size in samples.
//
// It panics on a size outside [2, MaxWindowSize]: a one-sample window has no
// slope, and a larger one would overflow the int64 accumulators. Both are
// programming errors in the caller, not conditions to be handled at runtime.
func NewWindow(size int) Window {
	if size < 2 || size > MaxWindowSize {
		panic("stats: window size " + strconv.Itoa(size) + " outside [2, " +
			strconv.Itoa(MaxWindowSize) + "] — a smaller window has no slope and a " +
			"larger one overflows the int64 accumulators")
	}
	return Window{size: int64(size)}
}

// Add accumulates one population sample and reports whether the window is now
// full and ready to be evaluated.
//
// It panics on a sample outside [0, MaxSample]; see MaxSample for why that is
// an assertion rather than a clamp.
func (w *Window) Add(sample int) bool {
	if sample < 0 || sample > MaxSample {
		panic("stats: population sample " + strconv.Itoa(sample) + " outside [0, " +
			strconv.Itoa(MaxSample) + "] — every accumulator overflow bound depends on this limit")
	}
	if w.count >= w.size {
		panic("stats: Add on a window that is already full — evaluate and Reset first")
	}

	population := int64(sample)
	w.sumP += population
	w.sumPP += population * population
	// The index is the position within THIS window, so it restarts at zero
	// after every Reset. Both slope predicates are relative to the window, so
	// a running global index would give the same slope but a different (and
	// meaningless) intercept.
	w.sumIP += w.count * population
	w.count++

	return w.count == w.size
}

// Reset empties the window, keeping its size. Called on every boundary, which
// is what makes the windows non-overlapping.
func (w *Window) Reset() {
	w.count = 0
	w.sumP = 0
	w.sumPP = 0
	w.sumIP = 0
}

// Size is the number of samples the window holds when full.
func (w Window) Size() int { return int(w.size) }

// Count is the number of samples accumulated since the last Reset.
func (w Window) Count() int { return int(w.count) }

// Full reports whether the window has as many samples as its size.
func (w Window) Full() bool { return w.count == w.size }

// Sum is S1, the sum of the samples.
func (w Window) Sum() int64 { return w.sumP }

// SumOfSquares is S2, the sum of the squared samples.
func (w Window) SumOfSquares() int64 { return w.sumPP }

// SumOfIndexProducts is Sxy, the sum of each sample times its index.
func (w Window) SumOfIndexProducts() int64 { return w.sumIP }

// Sx is Σ i over the samples accumulated so far, n(n-1)/2.
func (w Window) Sx() int64 {
	return w.count * (w.count - 1) / 2
}

// Sxx is Σ i² over the samples accumulated so far, (n-1)n(2n-1)/6.
func (w Window) Sxx() int64 {
	return (w.count - 1) * w.count * (2*w.count - 1) / 6
}

// slopeNumerator is N = n·Sxy − Sx·S1, the numerator of the least-squares
// slope of population against index. It carries the sign of the slope.
func (w Window) slopeNumerator() int64 {
	return w.count*w.sumIP - w.Sx()*w.sumP
}

// slopeDenominator is D = n·Sxx − Sx², which reduces to n²(n²−1)/12 and is
// therefore strictly positive for n ≥ 2. Because D > 0 always, every predicate
// below can multiply through by it without tracking a sign.
func (w Window) slopeDenominator() int64 {
	return w.count*w.Sxx() - w.Sx()*w.Sx()
}

// StableVariation reports whether the coefficient of variation is below 0.10.
//
//	stddev/mean < 1/10
//	⟺ (n·S2 − S1²)/S1² < 1/100
//	⟺ 100·n·S2 < 101·S1²
//
// Squaring both sides removes the square root, which is what makes the test
// exact in integers. Fits in int64 for any window within MaxWindowSize.
func (w Window) StableVariation() bool {
	if w.count < 1 || w.sumP <= 0 {
		return false
	}
	return 100*w.count*w.sumPP < 101*w.sumP*w.sumP
}

// HighVariation reports whether the coefficient of variation is above 0.25.
//
//	stddev/mean > 1/4  ⟺  16·n·S2 > 17·S1²
//
// A run that never settles is recognised by having had at least one such
// window; see Classifier.Finish.
func (w Window) HighVariation() bool {
	if w.count < 1 || w.sumP <= 0 {
		return false
	}
	return 16*w.count*w.sumPP > 17*w.sumP*w.sumP
}

// FlatSlope reports whether the window's trend, expressed as the fraction of
// the window mean gained or lost across the whole window, is within ±0.05.
//
//	|slope·n/mean| < 1/20  ⟺  20·n²·|N| < D·S1
//
// The left-hand side reaches ≈7.6e23 at MaxWindowSize, so the comparison goes
// through mulCmpU128 rather than int64 multiplication.
func (w Window) FlatSlope() bool {
	if w.count < 2 || w.sumP <= 0 {
		return false
	}
	magnitude := w.slopeNumerator()
	if magnitude < 0 {
		magnitude = -magnitude
	}
	return mulCmpU128(
		uint64(20*w.count*w.count), uint64(magnitude),
		uint64(w.slopeDenominator()), uint64(w.sumP),
	) < 0
}

// DecliningSlope reports whether the window lost more than 10 % of its own mean
// population across the window.
//
//	slope·n/mean < −1/10  ⟺  N < 0 and 10·n²·|N| > D·S1
//
// The sign is tested on N alone because D and S1 are both positive here.
func (w Window) DecliningSlope() bool {
	if w.count < 2 || w.sumP <= 0 {
		return false
	}
	numerator := w.slopeNumerator()
	if numerator >= 0 {
		return false
	}
	return mulCmpU128(
		uint64(10*w.count*w.count), uint64(-numerator),
		uint64(w.slopeDenominator()), uint64(w.sumP),
	) > 0
}
