package stats

// Recorder is a Classifier that also keeps the full per-tick population
// history, which the viewer draws as a sparkline.
//
// The batch harness uses a bare Classifier: it needs the outcome and the peak,
// never the trajectory, and a 12 000-entry history per run would be pure
// overhead. The viewer needs the trajectory. Rather than duplicate the peak
// tracking on both paths, the Recorder wraps a Classifier and delegates every
// question about the run to it — there is exactly one implementation of the
// state machine, and history is the only thing this type adds.
type Recorder struct {
	classifier *Classifier
	history    []int
}

// NewRecorder returns a recorder over a fresh classifier. expectedTicks sizes
// the history buffer up front; it is a hint, and a run that outlives it simply
// grows the slice.
func NewRecorder(params ClassifierConfig, expectedTicks int) *Recorder {
	if expectedTicks < 0 {
		expectedTicks = 0
	}
	return &Recorder{
		classifier: NewClassifier(params),
		history:    make([]int, 0, expectedTicks),
	}
}

// Observe records the population after one completed tick: it appends to the
// history and feeds the classifier.
//
// History is appended even after the classifier goes terminal, so a viewer
// replaying past the point of resolution still draws a continuous curve.
func (r *Recorder) Observe(population int) {
	r.history = append(r.history, population)
	r.classifier.Observe(population)
}

// History is the population after every observed tick, index 0 being tick 1.
// The slice is owned by the recorder; callers must not modify it.
func (r *Recorder) History() []int { return r.history }

// Classifier is the underlying state machine, for callers that want an
// accessor this type does not forward.
func (r *Recorder) Classifier() *Classifier { return r.classifier }

// Finish resolves the run and returns the verdict. See Classifier.Finish.
func (r *Recorder) Finish() Outcome { return r.classifier.Finish() }

// Outcome is the verdict so far. See Classifier.Outcome.
func (r *Recorder) Outcome() Outcome { return r.classifier.Outcome() }

// Done reports whether the outcome is settled. See Classifier.Done.
func (r *Recorder) Done() bool { return r.classifier.Done() }

// Tick is the number of ticks observed.
func (r *Recorder) Tick() int { return r.classifier.Tick() }

// FinalPopulation is the population of the most recently observed tick.
func (r *Recorder) FinalPopulation() int { return r.classifier.FinalPopulation() }

// PeakPopulation is the highest population observed on a non-terminal tick.
func (r *Recorder) PeakPopulation() int { return r.classifier.PeakPopulation() }

// PeakTick is the tick at which PeakPopulation was first reached.
func (r *Recorder) PeakTick() int { return r.classifier.PeakTick() }

// PeakYear is PeakTick expressed in simulated years.
func (r *Recorder) PeakYear() int { return r.classifier.PeakYear() }

// ExtinctYear is the year the population reached zero, or -1 if it never did.
func (r *Recorder) ExtinctYear() int { return r.classifier.ExtinctYear() }
