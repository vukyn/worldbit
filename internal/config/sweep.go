package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/vukyn/worldbit/internal/sim"
)

// Sweep is a parameter grid: a set of axes, each with a list of values, run
// over a range of seeds.
//
// Seed variation alone produces near-identical runs at a fixed parameter set —
// the variety lives in the parameters — so this is what turns the harness from
// a determinism demonstration into an experiment.
//
// The file format:
//
//	{
//	  "seeds": 50,
//	  "seed_start": 1,
//	  "axes": {
//	    "InitFoodPerCell": [1, 2, 3, 5],
//	    "ReproEnergyCost": [30, 40, 50]
//	  }
//	}
//
// Axis keys are Config field names, the same PascalCase identifiers --set
// takes, resolved through the same SetField. An unknown key is a hard error, as
// it is for --config and --set: a silently ignored axis would produce a grid
// of identical cells that looks entirely plausible.
type Sweep struct {
	// Seeds is how many consecutive seeds each cell runs, and SeedStart the
	// first of them.
	Seeds     int
	SeedStart uint64

	// names is the axis order: sorted, because the file's axes arrive in a
	// JSON object and Go map iteration is deliberately random. Everything
	// downstream — cell numbering, column order, the aggregate — depends on
	// this order being fixed.
	names []string
	// values holds each axis's values as the canonical text SetField accepts.
	values map[string][]string
}

// SweepAxis is one axis name bound to the value a cell uses.
type SweepAxis struct {
	Name  string
	Value string
}

// SweepCell is a resolved, validated point of the grid.
type SweepCell struct {
	// Index is the position in the full Cartesian product, so it still
	// identifies the same cell when earlier cells were skipped.
	Index  int
	Config sim.Config
	Axes   []SweepAxis
}

// SkippedCell is a grid point whose config did not validate. It is reported and
// skipped rather than aborting the sweep: one impossible corner of a grid — say
// ReproEnergyCost above ReproEnergyMin — should not throw away every other cell.
type SkippedCell struct {
	Index int
	Axes  []SweepAxis
	Err   error
}

// LoadSweep reads and validates a sweep file. Every check that does not depend
// on the base config happens here, so a malformed grid fails before a single
// tick runs.
//
// seed_start is decoded through a pointer so that an absent key defaults to 1
// while an explicit 0 stays 0 — seed 0 is a legal seed.
func LoadSweep(path string) (*Sweep, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read sweep %s: %w", path, err)
	}
	defer file.Close()

	var document struct {
		Seeds     *int             `json:"seeds"`
		SeedStart *uint64          `json:"seed_start"`
		Axes      map[string][]any `json:"axes"`
	}

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	// UseNumber keeps numeric values as their literal text, so an axis value
	// reaches SetField exactly as written. Without it 5 would arrive as the
	// float64 5 and be re-rendered, and a value that does not fit its field
	// would be silently rounded instead of rejected.
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parse sweep %s: %w", path, err)
	}

	sweep := &Sweep{Seeds: 1, SeedStart: 1, values: make(map[string][]string)}
	if document.Seeds != nil {
		sweep.Seeds = *document.Seeds
	}
	if document.SeedStart != nil {
		sweep.SeedStart = *document.SeedStart
	}
	if sweep.Seeds < 1 {
		return nil, fmt.Errorf("sweep %s: seeds must be positive, got %d", path, sweep.Seeds)
	}
	if len(document.Axes) == 0 {
		return nil, fmt.Errorf("sweep %s: axes is empty; a sweep with no axes is just a seed batch", path)
	}

	for name := range document.Axes {
		sweep.names = append(sweep.names, name)
	}
	sort.Strings(sweep.names)

	// A probe config catches both halves of an unusable axis up front: a key
	// that names no parameter, and a value that cannot be applied to the
	// parameter it names. Both would fail identically in every cell, so
	// reporting them once, before anything runs, beats reporting them
	// cells-many times as skips.
	probe := sim.DefaultConfig()
	for _, name := range sweep.names {
		rawValues := document.Axes[name]
		if len(rawValues) == 0 {
			return nil, fmt.Errorf("sweep %s: axis %s has no values", path, name)
		}
		for _, rawValue := range rawValues {
			text, err := axisValueText(rawValue)
			if err != nil {
				return nil, fmt.Errorf("sweep %s: axis %s: %w", path, name, err)
			}
			if err := SetField(&probe, name, text); err != nil {
				return nil, fmt.Errorf("sweep %s: axis %w", path, err)
			}
			sweep.values[name] = append(sweep.values[name], text)
		}
	}

	return sweep, nil
}

// axisValueText renders one JSON axis value as the text SetField parses.
func axisValueText(value any) (string, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.String(), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("value %v has type %T; expected a number, a boolean or a string", value, value)
	}
}

// Names is the axis order used by cell numbering and every output column.
func (s *Sweep) Names() []string {
	names := make([]string, len(s.names))
	copy(names, s.names)
	return names
}

// CellCount is the size of the full Cartesian product, including cells that
// will be skipped as invalid.
func (s *Sweep) CellCount() int {
	total := 1
	for _, name := range s.names {
		total *= len(s.values[name])
	}
	return total
}

// Expand produces the Cartesian product of the axes on top of a base config.
//
// Validation runs per cell, and an invalid cell is returned as a SkippedCell
// rather than as an error: a grid deliberately spans territory the parameters
// cannot all reach, and losing the whole sweep to one impossible corner would
// make wide grids unusable.
func (s *Sweep) Expand(base sim.Config) ([]SweepCell, []SkippedCell) {
	total := s.CellCount()
	cells := make([]SweepCell, 0, total)
	skipped := make([]SkippedCell, 0)

	for index := 0; index < total; index++ {
		axes := s.axesAt(index)

		cfg := base
		failure := error(nil)
		for _, axis := range axes {
			if err := SetField(&cfg, axis.Name, axis.Value); err != nil {
				failure = err
				break
			}
		}
		if failure == nil {
			failure = cfg.Validate()
		}
		if failure != nil {
			skipped = append(skipped, SkippedCell{Index: index, Axes: axes, Err: failure})
			continue
		}

		cells = append(cells, SweepCell{Index: index, Config: cfg, Axes: axes})
	}

	return cells, skipped
}

// axesAt decodes a product index into one value per axis, odometer style with
// the last axis varying fastest.
func (s *Sweep) axesAt(index int) []SweepAxis {
	axes := make([]SweepAxis, len(s.names))
	remainder := index
	for position := len(s.names) - 1; position >= 0; position-- {
		name := s.names[position]
		values := s.values[name]
		axes[position] = SweepAxis{Name: name, Value: values[remainder%len(values)]}
		remainder /= len(values)
	}
	return axes
}

// DescribeAxes renders a cell's axis values for a log line: the shortest
// description that lets a reader reproduce the cell.
func DescribeAxes(axes []SweepAxis) string {
	if len(axes) == 0 {
		return "base config"
	}
	parts := make([]string, len(axes))
	for i, axis := range axes {
		parts[i] = axis.Name + "=" + axis.Value
	}
	return strings.Join(parts, ", ")
}
