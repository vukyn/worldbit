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
	// OutcomeOscillating means enough windows had a coefficient of variation
	// above 0.25 AT A POPULATION LARGE ENOUGH FOR THAT TO MEAN SOMETHING, and
	// the run never stabilised. See ClassifierConfig.MinOscillatingPopulation
	// for the population floor and why an unfloored version of this outcome
	// labelled starving remnants as oscillating ecologies, and
	// ClassifierConfig.MinOscillatingWindows for how many such windows "enough"
	// is and why one is not.
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

// StopsRun reports whether an outcome ends the simulation early.
//
// Extinction and overrun do: nothing further can be learned from a world with
// no agents, and an overrun run costs roughly thirteen times a normal one per
// tick, which across a sweep is the difference between minutes and hours.
//
// STABLE is terminal for the classifier but deliberately does NOT stop the run.
// FinalPop, FinalTick and StateHash describe the world where the simulation
// stopped; cutting stable runs short at their third stable boundary would make
// those three columns describe a different point in time for stable runs than
// for every other kind.
//
// It lives here rather than in the batch harness because the viewer must stop
// at exactly the same tick the harness did, or the replay hash it compares
// against a recorded state_hash would be taken from a different moment and
// every extinct run would report a spurious mismatch.
func StopsRun(outcome Outcome) bool {
	return outcome == OutcomeExtinct || outcome == OutcomeOverrun
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
	// MinOscillatingWindows is how many windows must clear the high-variation
	// predicate before a run may resolve as OSCILLATING — the temporal mirror
	// of StableWindows, which STABLE has always had to earn.
	//
	// Without it the flag LATCHES: one window clearing the predicate settles
	// the verdict for the whole run, so a single transient excursion reads as
	// sustained oscillation. Measured on the 1200-run FoodRegrowTicks x
	// BurnPerTick grid, that was not a corner case — 30 of the 108 OSCILLATING
	// runs rested on exactly ONE firing window out of nine evaluable ones, more
	// than twice any other bucket of the distribution:
	//
	//	firing windows  1   2   3   4   5   6   7   8   9
	//	runs           30  13   8   5   8  11  10   5  18
	//
	// The spike at one is the latch signature, and it is where three whole
	// sweep cells live that no reading of their trajectory calls a cycle. The
	// default of 2 is the smallest threshold that removes it, and it is chosen
	// against the SHAPE of that distribution rather than tuned: nothing
	// comparable happens at 2, 3 or 4, so a higher threshold buys no further
	// separation and starts costing genuine cycles. See DefaultClassifierConfig.
	//
	// The count is TOTAL firing windows, NOT a consecutive streak, and that is
	// the one place this rule deliberately departs from StableWindows. A
	// consecutive rule interacts with MinOscillatingPopulation: a cycle whose
	// mean sits near the population floor fails the floor in its trough
	// windows, so every trough resets the streak and a real cycle can never
	// build one. That is not hypothetical — the measured cell
	// FoodRegrowTicks=600 / BurnPerTick=5 (mean population 52-56 against a
	// floor of 64, 13-15 large excursions per run) keeps 18 of 20 runs under a
	// total rule of 2 and 4 of 20 under a consecutive rule of 2. Requiring
	// windows to be consecutive would make the verdict depend on where the
	// cycle happens to sit relative to a different threshold.
	//
	// One is the opt-out: it makes the test "at least one window fired", which
	// is exactly the latching behaviour, reproduced exactly. Values below one
	// are rejected; zero would resolve every unstable run as OSCILLATING, which
	// is not a weaker rule but a different and nonsensical one.
	MinOscillatingWindows int
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
// windows, a 20-agent stability floor, a 64-agent high-variation floor, two
// high-variation windows, one burn-in window, and the overrun threshold for the
// default 128x128 grid.
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
// to 87 while keeping 61 of the 68 genuinely cycling ones. (Those two counts
// predate the regrowth-permutation fix and are not comparable with the numbers
// below, which were measured after it.)
//
// The two-window sustained requirement is derived the same way, on the same
// 1200-run grid re-measured after that fix. It cuts OSCILLATING from 108 runs
// to 78, and every one of the 30 it removes had exactly one firing window. It
// leaves the genuine-cycle region — BurnPerTick=5 with FoodRegrowTicks 300-600,
// where the population swings around a stationary mean for the whole run — at
// 75 of its 80 runs, so it removes the latch without touching what the label is
// for. Against an independent trajectory-shape label (large excursions counted
// off the smoothed series, which knows nothing about the window grid) two is
// also the best of the candidates, at 12 disagreements out of 108 against 15
// for three and 23 for four. See ClassifierConfig.MinOscillatingWindows.
func DefaultClassifierConfig() ClassifierConfig {
	return ClassifierConfig{
		WindowTicks:              1200,
		TicksPerYear:             120,
		StableWindows:            3,
		MinStablePopulation:      20,
		MinOscillatingPopulation: 64,
		MinOscillatingWindows:    2,
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
	// so no running maximum is needed — which is what keeps this package free
	// of floats and big.Rat. What is needed is HOW MANY windows cleared the
	// line, because one is not enough to call a run oscillating; see
	// ClassifierConfig.MinOscillatingWindows.
	highVariationWindows int

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
	// One, not zero, is the opt-out here: zero would make the OSCILLATING
	// branch fire on a run with no high-variation window at all, which is not a
	// weaker rule but a different and nonsensical one.
	if params.MinOscillatingWindows < 1 {
		panic("stats: MinOscillatingWindows must be positive, got " +
			strconv.Itoa(params.MinOscillatingWindows))
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
	// Deliberately a TOTAL count and not a consecutive streak, unlike the
	// stable streak above; see ClassifierConfig.MinOscillatingWindows.
	if c.window.HighVariation() && c.window.MeanAtLeast(c.params.MinOscillatingPopulation) {
		c.highVariationWindows++
	}

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
	case c.highVariationWindows >= c.params.MinOscillatingWindows:
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

// HighVariationWindows is how many post-burn-in windows cleared both the
// coefficient-of-variation line and the population floor. See
// ClassifierConfig.MinOscillatingWindows.
func (c *Classifier) HighVariationWindows() int { return c.highVariationWindows }
