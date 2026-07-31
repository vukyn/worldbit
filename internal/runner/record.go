// Package runner is the batch harness: it runs many simulations, classifies
// each one, and writes one record per run.
//
// Parallelism exists here and only here, and only BETWEEN runs. Each worker
// builds its own World and shares nothing with any other worker, so the
// simulation itself stays single-threaded and bit-exact. Results are sorted
// before they are written, which is what makes the output file byte-identical
// regardless of how many workers produced it.
//
// The package imports internal/sim and internal/stats and nothing else of the
// repository's own: the sweep's config-file handling lives in internal/config,
// so that the strict one-way dependency runner -> {sim, stats} holds.
package runner

// Axis is one swept parameter and the value this run used for it. The value is
// kept as the canonical text that was applied through the --set field
// resolution, so the record reads exactly like the command line that would
// reproduce it.
type Axis struct {
	Name  string
	Value string
}

// Record is everything known about one completed run: one CSV row, one JSON
// object.
//
// The eleven canonical columns are in the order the plan fixes. Sweep axis
// columns are appended AFTER them rather than mixed in, so a tool that knows
// the canonical schema keeps working on a sweep's output.
type Record struct {
	// Seed is the world seed. Together with Config (recorded in the sidecar)
	// and Version it is everything needed to reproduce the run exactly.
	Seed uint64
	// Outcome is the classifier's verdict, as its uppercase name.
	Outcome string
	// PeakPop is the highest population seen on a non-terminal tick, and
	// PeakYear the year it was first reached.
	PeakPop  int
	PeakYear int
	// ExtinctYear is the year the population reached zero, or -1.
	ExtinctYear int
	// FinalPop and FinalTick describe the world where the simulation actually
	// stopped. For a run cut short by a terminal outcome that is the tick the
	// outcome happened on, not MaxTick — the early stop is recorded, never
	// papered over.
	FinalPop  int
	FinalTick int
	// StateHash is the canonical simulation hash at FinalTick. Replaying the
	// seed to FinalTick must reproduce it; that is what makes the viewer's
	// replay a live determinism check.
	StateHash uint64
	// ConfigHash is the hash of the config THIS run used, which in a sweep is
	// the cell's config and not the base. Records with different ConfigHash
	// values are not comparable.
	ConfigHash uint64
	// Version is the binary version, so a "same seed, different hash"
	// investigation starts from the binary instead of from guesswork.
	Version string
	// WallMS is how long the run took. It is the one non-deterministic column
	// and must be excluded from any result-diff tool.
	WallMS int64

	// CellIndex is the sweep cell this run belongs to, and 0 for a plain seed
	// batch. It is the index into the full Cartesian product, so it keeps
	// identifying the same cell even when earlier cells were skipped as
	// invalid.
	CellIndex int
	// Axes are the cell's swept values, in the same order in every record of
	// one batch. Empty for a plain seed batch.
	Axes []Axis
}
