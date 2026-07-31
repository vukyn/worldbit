package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vukyn/worldbit/internal/sim"
)

func writeSweepFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sweep.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write sweep: %v", err)
	}
	return path
}

func loadSweep(t *testing.T, body string) *Sweep {
	t.Helper()
	sweep, err := LoadSweep(writeSweepFile(t, body))
	if err != nil {
		t.Fatalf("load sweep: %v", err)
	}
	return sweep
}

// TestSweepExpandsTheCartesianProduct covers the core of the feature: the grid
// size, the cell values, and the ordering that everything downstream — cell
// numbering, CSV column order, the aggregate — depends on.
func TestSweepExpandsTheCartesianProduct(t *testing.T) {
	sweep := loadSweep(t, `{
		"seeds": 5,
		"seed_start": 3,
		"axes": {
			"ReproEnergyCost": [30, 40],
			"InitFoodPerCell": [1, 2, 3]
		}
	}`)

	if sweep.Seeds != 5 || sweep.SeedStart != 3 {
		t.Errorf("seeds %d from %d, want 5 from 3", sweep.Seeds, sweep.SeedStart)
	}
	// Axis order is sorted, NOT the order the JSON object happened to list
	// them in: Go map iteration is random, so anything else would renumber the
	// cells between runs of the same sweep file.
	if want := []string{"InitFoodPerCell", "ReproEnergyCost"}; strings.Join(sweep.Names(), ",") != strings.Join(want, ",") {
		t.Errorf("axis order %v, want %v", sweep.Names(), want)
	}
	if sweep.CellCount() != 6 {
		t.Errorf("cell count %d, want 6", sweep.CellCount())
	}

	cells, skipped := sweep.Expand(sim.DefaultConfig())
	if len(skipped) != 0 {
		t.Fatalf("skipped %d cells, want none: %v", len(skipped), skipped)
	}
	if len(cells) != 6 {
		t.Fatalf("expanded %d cells, want 6", len(cells))
	}

	// The last axis varies fastest, odometer style.
	want := []string{"1/30", "1/40", "2/30", "2/40", "3/30", "3/40"}
	for index, cell := range cells {
		if cell.Index != index {
			t.Errorf("cell %d reports index %d", index, cell.Index)
		}
		got := cell.Axes[0].Value + "/" + cell.Axes[1].Value
		if got != want[index] {
			t.Errorf("cell %d is %s, want %s", index, got, want[index])
		}
		if int(cell.Config.InitFoodPerCell) != int(cell.Axes[0].Value[0]-'0') {
			t.Errorf("cell %d config InitFoodPerCell = %d, out of step with its axis value %s",
				index, cell.Config.InitFoodPerCell, cell.Axes[0].Value)
		}
	}
}

// TestSweepCellsCarryDistinctConfigHashes is what makes a sweep's records
// self-describing: every row's config_hash must be its own cell's, never the
// base config's, or two cells' results would look interchangeable.
func TestSweepCellsCarryDistinctConfigHashes(t *testing.T) {
	sweep := loadSweep(t, `{"seeds": 1, "axes": {"InitFoodPerCell": [1, 2, 3]}}`)
	cells, _ := sweep.Expand(sim.DefaultConfig())

	seen := make(map[uint64]bool, len(cells))
	for _, cell := range cells {
		hash := cell.Config.Hash()
		if seen[hash] {
			t.Errorf("cell %d reuses config hash %016x", cell.Index, hash)
		}
		seen[hash] = true
	}
	if len(seen) != 3 {
		t.Errorf("%d distinct config hashes over 3 cells", len(seen))
	}
}

// TestSweepRejectsUnknownAxisKey holds sweeps to the same rule as --config and
// --set. A silently ignored axis would produce a grid of identical cells that
// looks entirely plausible and answers a question nobody asked.
func TestSweepRejectsUnknownAxisKey(t *testing.T) {
	_, err := LoadSweep(writeSweepFile(t, `{"seeds": 1, "axes": {"FoodMaxx": [1, 2]}}`))
	if err == nil {
		t.Fatal("an unknown axis key was accepted")
	}
	if !strings.Contains(err.Error(), "FoodMaxx") {
		t.Errorf("error %q does not name the offending key", err)
	}
	if !strings.Contains(err.Error(), "unknown parameter") {
		t.Errorf("error %q does not say the key is unknown", err)
	}
}

// TestSweepRejectsUnusableAxisValues covers the values that could never apply
// to any cell. These are reported once, at load, rather than as one skip per
// cell: the axis is the thing that is wrong, not the cells.
func TestSweepRejectsUnusableAxisValues(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"value out of range for the field", `{"axes": {"FoodMax": [1, 99999]}}`, "FoodMax"},
		{"non-integer value", `{"axes": {"FoodMax": [1.5]}}`, "FoodMax"},
		{"wrong value type", `{"axes": {"FoodMax": [[1]]}}`, "FoodMax"},
		{"empty axis", `{"axes": {"FoodMax": []}}`, "no values"},
		{"no axes", `{"seeds": 4}`, "axes is empty"},
		{"unknown document key", `{"seedz": 4, "axes": {"FoodMax": [1]}}`, "seedz"},
		{"non-positive seeds", `{"seeds": 0, "axes": {"FoodMax": [1]}}`, "seeds must be positive"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := LoadSweep(writeSweepFile(t, testCase.body))
			if err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("error %q does not mention %q", err, testCase.want)
			}
		})
	}
}

// TestSweepSkipsInvalidCellsWithoutAborting is the behaviour that makes wide
// grids usable. A grid deliberately spans territory the parameters cannot all
// reach; losing every cell to one impossible corner would defeat the point.
func TestSweepSkipsInvalidCellsWithoutAborting(t *testing.T) {
	// ReproEnergyCost must not exceed ReproEnergyMin, which defaults to 60, so
	// the 80 column is invalid at every InitFoodPerCell.
	sweep := loadSweep(t, `{
		"seeds": 2,
		"axes": {
			"InitFoodPerCell": [1, 2],
			"ReproEnergyCost": [40, 80]
		}
	}`)

	cells, skipped := sweep.Expand(sim.DefaultConfig())
	if len(cells) != 2 {
		t.Fatalf("expanded %d valid cells, want 2", len(cells))
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped %d cells, want 2", len(skipped))
	}

	// A skipped cell must name itself and its reason well enough to act on.
	for _, cell := range skipped {
		if cell.Err == nil {
			t.Fatalf("skipped cell %d has no reason", cell.Index)
		}
		if !strings.Contains(cell.Err.Error(), "ReproEnergyCost") {
			t.Errorf("cell %d reason %q does not name the offending field", cell.Index, cell.Err)
		}
		if !strings.Contains(DescribeAxes(cell.Axes), "ReproEnergyCost=80") {
			t.Errorf("cell %d description %q does not show its axis values",
				cell.Index, DescribeAxes(cell.Axes))
		}
	}

	// Surviving cells keep their index in the FULL product, so a cell's
	// identity does not shift when a neighbour is dropped.
	if cells[0].Index != 0 || cells[1].Index != 2 {
		t.Errorf("surviving cell indices %d and %d, want 0 and 2",
			cells[0].Index, cells[1].Index)
	}
}

// TestSweepSeedStartDefaultsWithoutSwallowingZero covers the one place a
// pointer field earns its keep: an absent seed_start means 1, but an explicit 0
// must stay 0, because seed 0 is a perfectly legal seed.
func TestSweepSeedStartDefaultsWithoutSwallowingZero(t *testing.T) {
	if got := loadSweep(t, `{"axes": {"FoodMax": [5]}}`).SeedStart; got != 1 {
		t.Errorf("absent seed_start gave %d, want 1", got)
	}
	if got := loadSweep(t, `{"seed_start": 0, "axes": {"FoodMax": [5]}}`).SeedStart; got != 0 {
		t.Errorf("explicit seed_start 0 gave %d, want 0", got)
	}
	if got := loadSweep(t, `{"axes": {"FoodMax": [5]}}`).Seeds; got != 1 {
		t.Errorf("absent seeds gave %d, want 1", got)
	}
}

// TestSweepAppliesOnTopOfTheBaseConfig checks the resolution order: a sweep
// overrides its axes and nothing else, so --config and --set still govern every
// parameter the grid does not name.
func TestSweepAppliesOnTopOfTheBaseConfig(t *testing.T) {
	base := sim.DefaultConfig()
	base.MaxTick = 2400
	base.FoodMax = 9
	base.InitFoodPerCell = 9

	sweep := loadSweep(t, `{"axes": {"InitFoodPerCell": [1, 2]}}`)
	cells, skipped := sweep.Expand(base)
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}

	for _, cell := range cells {
		if cell.Config.MaxTick != 2400 {
			t.Errorf("cell %d lost the base MaxTick: %d", cell.Index, cell.Config.MaxTick)
		}
		if cell.Config.FoodMax != 9 {
			t.Errorf("cell %d lost the base FoodMax: %d", cell.Index, cell.Config.FoodMax)
		}
	}
	if cells[0].Config.InitFoodPerCell != 1 || cells[1].Config.InitFoodPerCell != 2 {
		t.Errorf("axis did not override the base: %d, %d",
			cells[0].Config.InitFoodPerCell, cells[1].Config.InitFoodPerCell)
	}
}

// TestSweepUsesTheSetFieldResolution is the anti-drift check. Axis keys and
// --set keys are the same names because they go through the same function; if
// somebody added a second name-matching path, the two would eventually accept
// different spellings and a sweep would stop reproducing from its own
// command line.
func TestSweepUsesTheSetFieldResolution(t *testing.T) {
	sweep := loadSweep(t, `{"axes": {"BurnInWindows": [0, 2]}}`)
	cells, _ := sweep.Expand(sim.DefaultConfig())

	for _, cell := range cells {
		viaSet := sim.DefaultConfig()
		if err := ApplyOverrides(&viaSet, []string{"BurnInWindows=" + cell.Axes[0].Value}); err != nil {
			t.Fatalf("--set BurnInWindows=%s: %v", cell.Axes[0].Value, err)
		}
		if viaSet.Hash() != cell.Config.Hash() {
			t.Errorf("cell %d config differs from the equivalent --set: %016x vs %016x",
				cell.Index, cell.Config.Hash(), viaSet.Hash())
		}
	}
}
