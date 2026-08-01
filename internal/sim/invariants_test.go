package sim

import "testing"

// emptyWorld builds a world with no agents at all, for the tests that pin
// phase 1 in isolation.
//
// Removing the founders is enough to keep it agent-free forever: nothing but a
// birth creates an agent, and a birth needs a parent. Config.Validate rejects
// InitAgents = 0, so this is the honest way to get an empty world without
// loosening a rule that exists for real runs.
func emptyWorld(t *testing.T, cfg Config) *World {
	t.Helper()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}

	world := NewWorld(1, cfg)
	world.Agents = world.Agents[:0]
	return world
}

// regrowPositions inverts the regrowth permutation: positionOf[cellIndex] is
// the position in regrowOrder the stride reaches that cell at.
//
// Inverting it also checks it: a slice that is not a permutation leaves a
// position unwritten or writes one twice, and both are caught here rather than
// showing up later as a coverage test that fails for an unrelated-looking
// reason.
func regrowPositions(t *testing.T, world *World) []int32 {
	t.Helper()

	cellCount := int32(len(world.Cells))
	if int32(len(world.regrowOrder)) != cellCount {
		t.Fatalf("regrowOrder has %d entries, want one per cell (%d)", len(world.regrowOrder), cellCount)
	}

	positionOf := make([]int32, cellCount)
	for i := range positionOf {
		positionOf[i] = -1
	}

	for position, index := range world.regrowOrder {
		if index < 0 || index >= cellCount {
			t.Fatalf("regrowOrder position %d holds cell %d, out of range", position, index)
		}
		if positionOf[index] != -1 {
			t.Fatalf("cell %d appears at regrowth positions %d and %d — not a permutation",
				index, positionOf[index], position)
		}
		positionOf[index] = int32(position)
	}

	return positionOf
}

// TestRegrowthOrderIsSpatiallyIncoherent is the assertion the permutation was
// added for, and the one a plain index stride fails outright.
//
// Striding over raw indices makes regrowth time an affine function of position:
// horizontally adjacent cells regrow exactly one tick apart, so every one of the
// 16384 horizontal neighbour pairs has a regrowth-position gap of 1 and the
// board recovers behind a travelling front. Under a uniform random permutation
// the expected number of adjacent pairs landing one position apart is about 2
// per direction, so the bound below is loose by orders of magnitude in the
// direction that matters: it cannot pass for anything with structure in it.
func TestRegrowthOrderIsSpatiallyIncoherent(t *testing.T) {
	cfg := DefaultConfig()
	world := emptyWorld(t, cfg)
	positionOf := regrowPositions(t, world)

	// A hundredth of the board. A travelling front scores 16384 here; chance
	// scores single digits.
	limit := int32(len(world.Cells)) / 100

	for _, direction := range []struct {
		name   string
		deltaX int32
		deltaY int32
	}{
		{name: "horizontal", deltaX: 1},
		{name: "vertical", deltaY: 1},
	} {
		consecutive := int32(0)
		for y := int32(0); y < cfg.Height; y++ {
			for x := int32(0); x < cfg.Width; x++ {
				here := world.cellIdx(uint8(x), uint8(y))
				there := world.cellIdx(world.wrapX(x+direction.deltaX), world.wrapY(y+direction.deltaY))

				gap := positionOf[here] - positionOf[there]
				if gap == 1 || gap == -1 {
					consecutive++
				}
			}
		}

		if consecutive > limit {
			t.Errorf("%d of %d %s neighbour pairs regrow one tick apart (limit %d) — regrowth is "+
				"sweeping the board as a travelling front, which is the artefact the permutation removes",
				consecutive, len(world.Cells), direction.name, limit)
		}
	}
}

// TestRegrowthOrderDoesNotDependOnTheSeed pins the half of the design that makes
// TestRegrowthOrderIsSpatiallyIncoherent worth anything.
//
// The permutation is a constant of the simulator, so the incoherence test above
// checks the exact schedule every run will ever use rather than sampling one
// seed's draw. If this ever starts failing, that test has quietly turned into a
// spot check.
func TestRegrowthOrderDoesNotDependOnTheSeed(t *testing.T) {
	cfg := DefaultConfig()
	reference := NewWorld(1, cfg)

	for _, seed := range []uint64{0, 2, 7, 12345, 1 << 63} {
		other := NewWorld(seed, cfg)
		for i := range reference.regrowOrder {
			if reference.regrowOrder[i] != other.regrowOrder[i] {
				t.Fatalf("seeds 1 and %d disagree on regrowth position %d (%d vs %d) — the "+
					"permutation must not depend on the seed", seed, i,
					reference.regrowOrder[i], other.regrowOrder[i])
			}
		}
	}

	// A different grid size is a different permutation, and must still be one.
	small := cfg
	small.Width, small.Height = 16, 16
	small.SearchRadius = 8
	regrowPositions(t, NewWorld(1, small))
}

// TestRegrowthStrideCoversEachCellExactlyOnce pins the stride scheme: over one
// full FoodRegrowTicks period every cell regrows exactly once, and no tick
// touches a cell twice. A synchronised global pulse would pass a naive
// "food goes up" test while stamping a sawtooth on every run, so the assertion
// is per-cell and per-tick, not aggregate.
//
// The stride now runs over positions in the regrowth permutation rather than
// over cell indices, so the per-tick assertion is on the cell's POSITION. The
// coverage guarantee is unchanged and is exactly what a permutation preserves.
func TestRegrowthStrideCoversEachCellExactlyOnce(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FoodMax = 100 // high enough that no cell can saturate during the period
	cfg.InitFoodPerCell = 1

	world := emptyWorld(t, cfg)
	cellCount := int32(len(world.Cells))
	stride := cfg.FoodRegrowTicks
	positionOf := regrowPositions(t, world)

	regrowCount := make([]int, cellCount)
	before := make([]int16, cellCount)

	lowPerTick := cellCount / stride
	highPerTick := lowPerTick
	if cellCount%stride != 0 {
		highPerTick++
	}

	for tick := int32(0); tick < stride; tick++ {
		for i := range world.Cells {
			before[i] = world.Cells[i].Food
		}

		Step(world)

		changed := int32(0)
		for i := range world.Cells {
			delta := world.Cells[i].Food - before[i]
			switch delta {
			case 0:
			case 1:
				changed++
				regrowCount[i]++
				if positionOf[i]%stride != tick {
					t.Fatalf("tick %d regrew cell %d (regrowth position %d), which is not on the stride",
						tick, i, positionOf[i])
				}
			default:
				t.Fatalf("tick %d changed cell %d by %d, expected 0 or 1", tick, i, delta)
			}
		}

		if changed < lowPerTick || changed > highPerTick {
			t.Fatalf("tick %d regrew %d cells, expected %d or %d", tick, changed, lowPerTick, highPerTick)
		}
	}

	for i, count := range regrowCount {
		if count != 1 {
			t.Fatalf("cell %d regrew %d times in one period, expected exactly 1", i, count)
		}
	}
}

// TestFoodNeverExceedsMaxOrGoesNegative checks the food bound on every cell on
// every tick, including well past full saturation.
func TestFoodNeverExceedsMaxOrGoesNegative(t *testing.T) {
	cfg := DefaultConfig()
	world := emptyWorld(t, cfg)

	// Enough ticks to saturate the board several times over: every cell
	// regrows once per 200 ticks, so 200 * FoodMax ticks fills it.
	totalTicks := cfg.FoodRegrowTicks * int32(cfg.FoodMax) * 2

	for tick := int32(0); tick < totalTicks; tick++ {
		Step(world)
		for i := range world.Cells {
			food := world.Cells[i].Food
			if food < 0 {
				t.Fatalf("tick %d: cell %d has negative food %d", world.Tick, i, food)
			}
			if food > cfg.FoodMax {
				t.Fatalf("tick %d: cell %d has food %d, above FoodMax %d", world.Tick, i, food, cfg.FoodMax)
			}
		}
	}

	// Saturation is total, and it is stable: once every cell is at FoodMax the
	// hash stops moving.
	for i := range world.Cells {
		if world.Cells[i].Food != cfg.FoodMax {
			t.Fatalf("cell %d is at %d after saturation, expected FoodMax %d", i, world.Cells[i].Food, cfg.FoodMax)
		}
	}
	saturated := make([]Cell, len(world.Cells))
	copy(saturated, world.Cells)
	Run(world, world.Tick+cfg.FoodRegrowTicks)
	for i := range world.Cells {
		if world.Cells[i] != saturated[i] {
			t.Fatalf("cell %d changed during a full regrowth period of a saturated world", i)
		}
	}
}

// TestNewWorldStartsClean checks the initial condition, which every golden
// hash is anchored to.
func TestNewWorldStartsClean(t *testing.T) {
	cfg := DefaultConfig()
	world := NewWorld(7, cfg)

	if world.Tick != 0 {
		t.Errorf("Tick = %d, want 0", world.Tick)
	}
	if world.Seed != 7 {
		t.Errorf("Seed = %d, want 7", world.Seed)
	}
	if got := len(world.Cells); got != int(cfg.Width*cfg.Height) {
		t.Errorf("len(Cells) = %d, want %d", got, cfg.Width*cfg.Height)
	}
	if got := int32(len(world.Agents)); got != cfg.InitAgents {
		t.Errorf("len(Agents) = %d, want InitAgents %d", got, cfg.InitAgents)
	}
	for i := range world.Cells {
		if world.Cells[i].Biome != BiomePlain {
			t.Fatalf("cell %d is not BiomePlain", i)
		}
		if world.Cells[i].Food != cfg.InitFoodPerCell {
			t.Fatalf("cell %d has food %d, want InitFoodPerCell %d", i, world.Cells[i].Food, cfg.InitFoodPerCell)
		}
	}
}

// TestTorusIndexRoundTrip checks the shift/mask index helpers against every
// coordinate on the default grid, and checks that wrapping is a true torus.
func TestTorusIndexRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	world := NewWorld(1, cfg)

	seen := make([]bool, len(world.Cells))
	for y := int32(0); y < cfg.Height; y++ {
		for x := int32(0); x < cfg.Width; x++ {
			index := world.cellIdx(uint8(x), uint8(y))
			if index < 0 || int(index) >= len(world.Cells) {
				t.Fatalf("cellIdx(%d,%d) = %d, out of range", x, y, index)
			}
			if seen[index] {
				t.Fatalf("cellIdx(%d,%d) = %d collides", x, y, index)
			}
			seen[index] = true

			if got := world.cellX(index); int32(got) != x {
				t.Fatalf("cellX(%d) = %d, want %d", index, got, x)
			}
			if got := world.cellY(index); int32(got) != y {
				t.Fatalf("cellY(%d) = %d, want %d", index, got, y)
			}
		}
	}

	if got := world.wrapX(cfg.Width); got != 0 {
		t.Errorf("wrapX(%d) = %d, want 0", cfg.Width, got)
	}
	if got := world.wrapX(-1); int32(got) != cfg.Width-1 {
		t.Errorf("wrapX(-1) = %d, want %d", got, cfg.Width-1)
	}
	if got := world.wrapY(cfg.Height + 3); got != 3 {
		t.Errorf("wrapY(%d) = %d, want 3", cfg.Height+3, got)
	}
	if got := world.wrapY(-1); int32(got) != cfg.Height-1 {
		t.Errorf("wrapY(-1) = %d, want %d", got, cfg.Height-1)
	}
}

// TestHashCoversCanonicalState checks that the digest actually moves when any
// piece of canonical state moves — a hash that ignores a field would make the
// whole golden-fixture apparatus decorative.
func TestHashCoversCanonicalState(t *testing.T) {
	cfg := DefaultConfig()
	base := NewWorld(1, cfg)
	baseline := Hash(base)

	mutations := []struct {
		name  string
		apply func(w *World)
	}{
		{"Tick", func(w *World) { w.Tick++ }},
		{"NextID", func(w *World) { w.NextID++ }},
		{"cell Food", func(w *World) { w.Cells[9999].Food++ }},
		{"cell Biome", func(w *World) { w.Cells[123].Biome = BiomeWater }},
		{"agent count", func(w *World) { w.Agents = append(w.Agents, Agent{ID: 1, TargetIdx: -1}) }},
		{"agent X", func(w *World) {
			w.Agents = append(w.Agents, Agent{ID: 1, X: 3, TargetIdx: -1})
		}},
		{"agent Energy", func(w *World) {
			w.Agents = append(w.Agents, Agent{ID: 1, Energy: 50, TargetIdx: -1})
		}},
		{"agent TargetIdx", func(w *World) {
			w.Agents = append(w.Agents, Agent{ID: 1, TargetIdx: 77})
		}},
	}

	previous := make([]uint64, 0, len(mutations))
	for _, mutation := range mutations {
		world := NewWorld(1, cfg)
		mutation.apply(world)
		mutated := Hash(world)
		if mutated == baseline {
			t.Errorf("mutating %s did not change the state hash", mutation.name)
		}
		for i, earlier := range previous {
			if earlier == mutated {
				t.Errorf("mutating %s collides with mutation %d", mutation.name, i)
			}
		}
		previous = append(previous, mutated)
	}
}
