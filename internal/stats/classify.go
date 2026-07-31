package stats

import "strconv"

// Outcome is the classification of a run. The six terminal values are the
// vocabulary the batch harness reports in; OutcomeRunning is the state of a
// classifier that has not resolved yet.
type Outcome uint8

const (
	// OutcomeRunning means no verdict has been reached: the run is still in
	// progress and Finish has not been called.
	OutcomeRunning Outcome = iota
	// OutcomeExtinct means the population reached zero. Terminal.
	OutcomeExtinct
	// OutcomeStable means three consecutive windows were low-variation and
	// flat, with a population above the floor. Terminal.
	OutcomeStable
	// OutcomeOscillating means at least one window had a coefficient of
	// variation above 0.25 AT A POPULATION LARGE ENOUGH FOR THAT TO MEAN
	// SOMETHING, and the run never stabilised. See
	// ClassifierConfig.MinOscillatingPopulation for the floor and why an
	// unfloored version of this outcome labelled starving remnants as
	// oscillating ecologies.
	OutcomeOscillating
	// OutcomeOverrun means the population passed the overrun threshold.
	// Terminal, like extinction: such a run costs an order of magnitude more
	// wall time per tick and tells you nothing further.
	OutcomeOverrun
	// OutcomeDeclining means the final window was losing more than 10 % of its
	// mean population per window.
	OutcomeDeclining
	// OutcomeTimeout means the run reached the tick limit without matching any
	// other description.
	OutcomeTimeout
)

// String returns the uppercase name used in the CSV and JSON records.
func (o Outcome) String() string {
	switch o {
	case OutcomeRunning:
		return "RUNNING"
	case OutcomeExtinct:
		return "EXTINCT"
	case OutcomeStable:
		return "STABLE"
	case OutcomeOscillating:
		return "OSCILLATING"
	case OutcomeOverrun:
		return "OVERRUN"
	case OutcomeDeclining:
		return "DECLINING"
	case OutcomeTimeout:
		return "TIMEOUT"
	default:
		return "Outcome(" + strconv.Itoa(int(o)) + ")"
	}
}

// ClassifierConfig holds the thresholds the state machine resolves against.
//
// It is separate from sim.Config on purpose: this package never imports the
// simulation, so the two grid-derived numbers — how many ticks make a year and
// what population counts as an overrun — are passed in by the caller.
type ClassifierConfig struct {
	// WindowTicks is the length of one evaluation window, in ticks. Windows do
	// not overlap; the predicates are evaluated once per completed window.
	WindowTicks int
	// TicksPerYear converts ticks to the years used in reporting.
	TicksPerYear int
	// StableWindows is how many consecutive stable windows resolve a run as
	// STABLE.
	StableWindows int
	// MinStablePopulation is the floor below which a window cannot count
	// towards stability, however flat it is: a handful of survivors coasting
	// towards extinction is not a stable ecology.
	MinStablePopulation int
	// MinOscillatingPopulation is the mirror of MinStablePopulation for the
	// high-variation flag: the mean population a window must reach before its
	// coefficient of variation is allowed to count as high variation, and
	// therefore before it can resolve a run as OSCILLATING.
	//
	// The coefficient of variation is SCALE-FREE, and that is the whole
	// problem. Demographic noise in a population of mean p has a standard
	// deviation of roughly sqrt(p), so its coefficient of variation is roughly
	// 1/sqrt(p). Against the fixed 0.25 line that means:
	//
	//	p =  16  ->  noise alone gives CV 0.25 — the line is AT the noise floor
	//	p =  64  ->  noise alone gives CV 0.125 — the line is at twice the noise
	//	p = 700  ->  noise alone gives CV 0.038 — the line is far above noise
	//
	// So below a mean of 16 the predicate cannot fail, and a starving remnant
	// of a dozen agents is reported as an oscillating ecology purely because
	// small numbers are noisy. Measurement bears this out: over a 1200-run
	// grid, 97 % of windows with a mean below 8 fire, and their measured
	// variation is indistinguishable from pure demographic noise, whereas
	// above a mean of 40 not one firing window is noise-explainable.
	//
	// The default of 64 is the point where the 0.25 line sits at twice the
	// demographic-noise level, so clearing it requires the population to be
	// genuinely twice as variable as chance. See DefaultClassifierConfig.
	//
	// The test is against the window MEAN, not the population at the boundary;
	// see Window.MeanAtLeast. Zero opts out entirely and reproduces the
	// un-floored behaviour exactly.
	MinOscillatingPopulation int
	// OverrunPopulation is the population that must be EXCEEDED to resolve a
	// run as OVERRUN. See OverrunPopulationFor.
	OverrunPopulation int
	// BurnInWindows is how many leading windows are excluded from EVERY
	// predicate: the stable streak, the high-variation flag and the
	// declining-slope fallback alike.
	//
	// A run opens with a startup transient — the founding population is fed by
	// the initial pantry, booms far past the carrying capacity and crashes back
	// — so the first window can never satisfy the stable predicate. Its
	// enormous coefficient of variation then latches the high-variation flag,
	// and OSCILLATING becomes the end-of-run fallback for every run that fails
	// to string together StableWindows stable windows. OSCILLATING would then
	// mean "did not reach STABLE in time" rather than "genuinely oscillating",
	// which mislabels whole regions of a parameter sweep.
	//
	// The excluded windows are still accumulated and still advance the
	// boundary, so window numbering and the tick timeline are unaffected. A
	// burn-in that some predicates respect and others ignore would be worse
	// than none, because the resulting verdict would answer no single question.
	//
	// Zero opts out entirely and reproduces the un-burnt-in behaviour exactly.
	// A value larger than the number of windows a run produces is legal: no
	// predicate is ever evaluated, and the run resolves as TIMEOUT.
	BurnInWindows int
}

// DefaultClassifierConfig returns the ratified thresholds: 1200-tick windows
// (ten years at the default 120 ticks per year), three consecutive stable
// windows, a 20-agent stability floor, a 64-agent high-variation floor, one
// burn-in window, and the overrun threshold for the default 128x128 grid.
//
// The 64-agent high-variation floor is derived, not chosen by feel. The
// predicate compares the coefficient of variation against 0.25; demographic
// noise alone produces a coefficient of variation of about 1/sqrt(p), so 64 is
// where 0.25 sits at exactly twice the noise level (1/sqrt(64) = 0.125) and a
// window clears the line only by being genuinely twice as variable as chance.
// 16 would be the point where the line sits AT the noise level, i.e. the
// weakest floor that is defensible at all.
//
// Measurement over a 1200-run FoodRegrowTicks x BurnPerTick grid agrees: 64
// maximises agreement with an independent label for sustained non-noise
// variation around a stationary mean, and the agreement curve is flat between
// 48 and 72, so the value is not knife-edge. It cuts OSCILLATING from 308 runs
// to 87 while keeping 61 of the 68 genuinely cycling ones.
func DefaultClassifierConfig() ClassifierConfig {
	return ClassifierConfig{
		WindowTicks:              1200,
		TicksPerYear:             120,
		StableWindows:            3,
		MinStablePopulation:      20,
		MinOscillatingPopulation: 64,
		OverrunPopulation:        OverrunPopulationFor(128 * 128),
		BurnInWindows:            1,
	}
}

// OverrunPopulationFor is 40 % of the cell count, rounded down — the threshold
// a population must exceed to count as an overrun. 6553 on the default grid.
//
// The result is floored at 1 so that a pathologically small grid (a 1x1 world
// is a legal, if absurd, configuration) yields a usable threshold instead of
// turning a config value into a panic in NewClassifier.
func OverrunPopulationFor(cellCount int) int {
	threshold := cellCount * 2 / 5
	if threshold < 1 {
		return 1
	}
	return threshold
}

// Classifier turns a stream of per-tick population counts into one Outcome.
//
// Call Observe once per completed tick, then Finish once at the tick limit.
// The four outcomes that can be recognised mid-run are terminal, so Done
// reports when there is nothing left to learn and the caller may stop
// simulating.
//
// Resolution order is part of the design, because the outcomes overlap: a run
// can be both wildly oscillating and finally extinct, and it is the extinction
// that is worth reporting. Per tick, extinction and overrun win over
// everything; on a window boundary, stability wins over the end-of-run
// fallbacks; and at the tick limit, oscillation wins over decline, which wins
// over an unremarkable timeout.
type Classifier struct {
	params ClassifierConfig
	window Window

	tick           int
	lastPopulation int
	peakPopulation int
	peakTick       int
	extinctTick    int

	outcome       Outcome
	stableStreak  int
	windowsClosed int

	// A window's coefficient of variation is only ever compared against 0.25,
	// so "the maximum CV over all windows exceeded 0.25" is exactly "some
	// window exceeded 0.25" — one bool instead of a rational running maximum
	// that could not be represented without floats or big.Rat.
	sawHighVariation bool

	// Likewise, only the LAST window's slope is consulted at the tick limit,
	// so the predicate is evaluated at each boundary and overwritten.
	lastWindowDeclining bool
}

// NewClassifier returns a classifier that has seen no ticks.
//
// It panics on a nonsensical configuration. These values come from validated
// simulation config or from DefaultClassifierConfig, so an invalid one is a
// programming error, and the alternative — silently classifying against
// thresholds nobody chose — is exactly the failure this project exists to
// prevent.
func NewClassifier(params ClassifierConfig) *Classifier {
	if params.TicksPerYear < 1 {
		panic("stats: TicksPerYear must be positive, got " + strconv.Itoa(params.TicksPerYear))
	}
	if params.StableWindows < 1 {
		panic("stats: StableWindows must be positive, got " + strconv.Itoa(params.StableWindows))
	}
	if params.OverrunPopulation < 1 {
		panic("stats: OverrunPopulation must be positive, got " + strconv.Itoa(params.OverrunPopulation))
	}
	if params.BurnInWindows < 0 {
		panic("stats: BurnInWindows must not be negative, got " + strconv.Itoa(params.BurnInWindows))
	}
	if params.MinOscillatingPopulation < 0 {
		panic("stats: MinOscillatingPopulation must not be negative, got " +
			strconv.Itoa(params.MinOscillatingPopulation))
	}

	return &Classifier{
		params:      params,
		window:      NewWindow(params.WindowTicks),
		extinctTick: -1,
		outcome:     OutcomeRunning,
	}
}

// Observe records the population after one completed tick. The nth call is
// tick n, so a caller that steps the world and then observes has the tick
// numbering the simulation itself uses.
//
// Calls after a terminal outcome are ignored, so a caller that keeps running
// past Done cannot change a verdict that has already been reached.
func (c *Classifier) Observe(population int) {
	if c.outcome != OutcomeRunning {
		return
	}

	// Asserted here as well as in Window.Add, because the overrun check below
	// returns before the sample ever reaches the window: without this, an
	// impossible population would quietly be filed as an ordinary OVERRUN
	// instead of exposing whatever produced it.
	if population < 0 || population > MaxSample {
		panic("stats: population " + strconv.Itoa(population) + " outside [0, " +
			strconv.Itoa(MaxSample) + "] — every accumulator overflow bound depends on this limit")
	}

	c.tick++
	c.lastPopulation = population

	// Terminal per-tick checks come first, before peak tracking and before the
	// window: both of these outcomes end the run on this tick, and neither
	// says anything about the window it interrupts. One consequence worth
	// knowing when reading a record: on an OVERRUN tick the peak is NOT
	// updated, so peak_pop describes the ticks the run completed and
	// final_pop carries the population that tripped the threshold.
	if population == 0 {
		c.extinctTick = c.tick
		c.outcome = OutcomeExtinct
		return
	}
	if population > c.params.OverrunPopulation {
		c.outcome = OutcomeOverrun
		return
	}

	if population > c.peakPopulation {
		c.peakPopulation = population
		c.peakTick = c.tick
	}

	if c.window.Add(population) {
		c.evaluateWindow(population)
		c.window.Reset()
	}
}

// evaluateWindow applies the boundary rules to the window that has just
// closed. population is the last sample in it, which is also the current
// population and therefore what the stability floor is tested against.
func (c *Classifier) evaluateWindow(population int) {
	index := c.windowsClosed
	c.windowsClosed++

	// Burn-in windows advance the boundary but answer no question. Returning
	// before every predicate — not just before the variation flag — is the
	// whole point: a burn-in that suppressed the high-variation latch while
	// still letting the startup crash reset the stable streak would delay
	// STABLE by exactly as many windows as it excluded, and a burn-in that
	// suppressed the streak but not the latch would still make OSCILLATING the
	// universal fallback. See ClassifierConfig.BurnInWindows.
	if index < c.params.BurnInWindows {
		return
	}

	// "Stable" is both halves of the specification's stable predicate: low
	// variation AND a flat trend. Low variation alone would accept a
	// population sliding smoothly and unrecoverably downwards.
	stable := c.window.StableVariation() && c.window.FlatSlope()

	if stable && population >= c.params.MinStablePopulation {
		c.stableStreak++
		if c.stableStreak >= c.params.StableWindows {
			c.outcome = OutcomeStable
		}
	} else {
		// The streak must be CONSECUTIVE: one unstable window in the middle of
		// four stable ones is a run that has not settled.
		c.stableStreak = 0
	}

	// A window only counts as high variation if it ALSO held enough agents for
	// the coefficient of variation to distinguish a real dynamic from
	// small-number noise. Without the floor, a starving remnant of a dozen
	// agents latches this flag and the run is reported as OSCILLATING; see
	// ClassifierConfig.MinOscillatingPopulation.
	c.sawHighVariation = c.sawHighVariation ||
		(c.window.HighVariation() && c.window.MeanAtLeast(c.params.MinOscillatingPopulation))
	c.lastWindowDeclining = c.window.DecliningSlope()
}

// Finish resolves a run that reached the tick limit without a terminal
// outcome, and returns the final verdict. It is idempotent, and returns the
// terminal outcome unchanged if one was already reached.
//
// The partially-filled window at the tick limit is deliberately discarded: it
// holds fewer samples than every other window, so its predicates are not
// comparable with theirs.
func (c *Classifier) Finish() Outcome {
	if c.outcome != OutcomeRunning {
		return c.outcome
	}

	switch {
	case c.sawHighVariation:
		c.outcome = OutcomeOscillating
	case c.lastWindowDeclining:
		c.outcome = OutcomeDeclining
	default:
		c.outcome = OutcomeTimeout
	}
	return c.outcome
}

// Outcome is the verdict so far: OutcomeRunning until a terminal outcome is
// reached or Finish is called.
func (c *Classifier) Outcome() Outcome { return c.outcome }

// Done reports whether the outcome is settled and the caller may stop
// simulating. Extinction, overrun and stability are all terminal.
func (c *Classifier) Done() bool { return c.outcome != OutcomeRunning }

// Tick is the number of ticks observed.
func (c *Classifier) Tick() int { return c.tick }

// FinalPopulation is the population of the most recently observed tick.
func (c *Classifier) FinalPopulation() int { return c.lastPopulation }

// PeakPopulation is the highest population observed on a non-terminal tick.
func (c *Classifier) PeakPopulation() int { return c.peakPopulation }

// PeakTick is the tick at which PeakPopulation was first reached.
func (c *Classifier) PeakTick() int { return c.peakTick }

// PeakYear is PeakTick expressed in simulated years.
func (c *Classifier) PeakYear() int { return c.peakTick / c.params.TicksPerYear }

// ExtinctYear is the year the population reached zero, or -1 if it never did.
func (c *Classifier) ExtinctYear() int {
	if c.extinctTick < 0 {
		return -1
	}
	return c.extinctTick / c.params.TicksPerYear
}

// WindowsClosed is the number of complete windows the run produced, INCLUDING
// the burn-in windows that no predicate was evaluated on.
func (c *Classifier) WindowsClosed() int { return c.windowsClosed }

// BurnInWindows is how many leading windows this classifier excludes from its
// predicates. See ClassifierConfig.BurnInWindows.
func (c *Classifier) BurnInWindows() int { return c.params.BurnInWindows }

// StableStreak is the number of consecutive stable windows ending at the most
// recent boundary.
func (c *Classifier) StableStreak() int { return c.stableStreak }
