package sim

import "math/bits"

// Biome values. Water is impassable and never regrows; it arrives behind a
// config knob in a later phase. Until then every cell is BiomePlain, but the
// field exists from the start so it is part of the canonical hash and no
// golden regeneration is needed when water lands.
const (
	BiomePlain uint8 = 0
	BiomeWater uint8 = 1
)

// Cell is one square of the torus grid.
type Cell struct {
	Biome uint8
	Food  int16
}

// Agent is one individual. Every field here is canonical state and is hashed.
type Agent struct {
	ID            uint32 // globally unique, monotonic, never reused
	X, Y          uint8  // torus coordinates
	Energy        int16  // 0..EnergyMax
	Age           int32
	LastBirthTick int32 // starts at birthTick - BirthCooldown, so newborns are simply immature
	TargetIdx     int32 // cached food target, -1 = none; influences behaviour, so it is canonical
}

// World is the entire simulation state.
//
// Agents is kept strictly ascending by ID at all times — that ordering is what
// makes intent resolution, death compaction and birth append deterministic
// without any sorting at all.
type World struct {
	Cfg    Config
	Seed   uint64
	Tick   int32
	Cells  []Cell // len = Width*Height, index = (y<<xShift)|x
	Agents []Agent
	NextID uint32

	// ---- derived, NOT hashed ----
	xShift uint  // log2(Width)
	xMask  int32 // Width-1
	yMask  int32 // Height-1
}

// NewWorld builds a world from a seed and an already-validated config.
//
// Config validation belongs to the caller (main resolves and validates before
// anything runs) — sim never reads files and never reports on I/O.
func NewWorld(seed uint64, cfg Config) *World {
	cellCount := int(cfg.Width) * int(cfg.Height)

	world := &World{
		Cfg:    cfg,
		Seed:   seed,
		Tick:   0,
		Cells:  make([]Cell, cellCount),
		Agents: make([]Agent, 0, cfg.InitAgents),
		NextID: 1,
		xShift: uint(bits.TrailingZeros32(uint32(cfg.Width))),
		xMask:  cfg.Width - 1,
		yMask:  cfg.Height - 1,
	}

	for i := range world.Cells {
		world.Cells[i].Biome = BiomePlain
		world.Cells[i].Food = cfg.InitFoodPerCell
	}

	return world
}

// Population is the current agent count.
func (w *World) Population() int32 { return int32(len(w.Agents)) }

// Year is the elapsed simulated year, for reporting only.
func (w *World) Year() int32 { return w.Tick / w.Cfg.TicksPerYear }

// WorldView is the read-only face of the world handed to the decide phase.
//
// It exposes value-returning accessors and no mutating methods at all, which
// makes "the decide phase cannot write" close to compiler-enforced rather than
// merely a convention.
type WorldView struct {
	world *World
}

// View returns the read-only view of this world.
func (w *World) View() WorldView { return WorldView{world: w} }

// Tick is the current tick.
func (v WorldView) Tick() int32 { return v.world.Tick }

// Seed is the world seed, the first input to every RNG substream.
func (v WorldView) Seed() uint64 { return v.world.Seed }

// Cfg is the resolved configuration.
func (v WorldView) Cfg() Config { return v.world.Cfg }

// Population is the current agent count.
func (v WorldView) Population() int32 { return int32(len(v.world.Agents)) }

// CellCount is the number of cells in the grid.
func (v WorldView) CellCount() int32 { return int32(len(v.world.Cells)) }

// Food is the food level of a cell by index.
func (v WorldView) Food(index int32) int16 { return v.world.Cells[index].Food }

// Biome is the biome of a cell by index.
func (v WorldView) Biome(index int32) uint8 { return v.world.Cells[index].Biome }

// CellIdx maps torus coordinates to a cell index.
func (v WorldView) CellIdx(x, y uint8) int32 { return v.world.cellIdx(x, y) }
