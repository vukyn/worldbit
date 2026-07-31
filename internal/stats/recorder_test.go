package stats

import "testing"

// TestRecorderKeepsHistoryAndDelegates covers the two things a Recorder adds
// over a bare Classifier: the full trajectory the viewer draws, and a single
// shared implementation of the state machine behind it.
func TestRecorderKeepsHistoryAndDelegates(t *testing.T) {
	params := DefaultClassifierConfig()
	samples := seriesOf(2000, func(index int) int { return 700 + index%50 })

	recorder := NewRecorder(params, len(samples))
	classifier := NewClassifier(params)

	for _, sample := range samples {
		recorder.Observe(sample)
		classifier.Observe(sample)
	}
	recorder.Finish()
	classifier.Finish()

	history := recorder.History()
	if len(history) != len(samples) {
		t.Fatalf("history holds %d samples, want %d", len(history), len(samples))
	}
	for index := range samples {
		if history[index] != samples[index] {
			t.Fatalf("history[%d] = %d, want %d", index, history[index], samples[index])
		}
	}

	if got, want := recorder.Outcome(), classifier.Outcome(); got != want {
		t.Errorf("recorder outcome %s, classifier outcome %s", got, want)
	}
	if got, want := recorder.PeakPopulation(), classifier.PeakPopulation(); got != want {
		t.Errorf("recorder peak %d, classifier peak %d", got, want)
	}
	if got, want := recorder.PeakTick(), classifier.PeakTick(); got != want {
		t.Errorf("recorder peak tick %d, classifier peak tick %d", got, want)
	}
	if got, want := recorder.Tick(), classifier.Tick(); got != want {
		t.Errorf("recorder tick %d, classifier tick %d", got, want)
	}
	if got, want := recorder.FinalPopulation(), classifier.FinalPopulation(); got != want {
		t.Errorf("recorder final population %d, classifier final population %d", got, want)
	}
	if got, want := recorder.ExtinctYear(), classifier.ExtinctYear(); got != want {
		t.Errorf("recorder extinct year %d, classifier extinct year %d", got, want)
	}
}

// TestRecorderKeepsHistoryPastTheVerdict: the classifier stops listening once
// an outcome is terminal, but a viewer replaying the run still needs a
// continuous curve to draw, so history keeps growing.
func TestRecorderKeepsHistoryPastTheVerdict(t *testing.T) {
	params := DefaultClassifierConfig()
	recorder := NewRecorder(params, 0)

	for tick := 0; tick < 100; tick++ {
		recorder.Observe(500)
	}
	recorder.Observe(0)
	if !recorder.Done() {
		t.Fatal("recorder is not done after the population reached zero")
	}

	for tick := 0; tick < 50; tick++ {
		recorder.Observe(0)
	}

	if got, want := len(recorder.History()), 151; got != want {
		t.Errorf("history holds %d samples, want %d — history must outlive the verdict", got, want)
	}
	if got, want := recorder.Tick(), 101; got != want {
		t.Errorf("classifier advanced to tick %d, want it frozen at %d", got, want)
	}
	if got, want := recorder.Outcome(), OutcomeExtinct; got != want {
		t.Errorf("outcome %s, want %s", got, want)
	}
	if recorder.Classifier() == nil {
		t.Error("Classifier() returned nil")
	}
}
