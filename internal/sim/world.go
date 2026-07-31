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

	// ---- derived / scratch, NOT hashed ----
	xShift uint  // log2(Width)
	xMask  int32 // Width-1
	yMask  int32 // Height-1

	// Coarse food index: total food per block of cells. Maintained
	// incrementally by regrowth and eating, rebuilt-and-compared under Verify.
	blockFood     []int32
	blockCols     int32
	blockRows     int32
	blockColShift uint
	blockScratch  []int32 // Verify's rebuild buffer, allocated on first use

	// Phase buffers, reused across ticks so a tick allocates nothing.
	intents []Intent
	births  []Birth

	// Per-tick energy bookkeeping and the previous tick's totals, which
	// together let Verify assert exact energy conservation.
	ledger          tickLedger
	prevTotalEnergy int64
	prevTotalFood   int64

	// First invariant violation seen, if Verify is enabled. Sticky: once the
	// state is known bad, later assertions are noise.
	verifyErr error
}

// tickLedger records every source and sink of energy for the current tick.
//
// Verify uses it to assert exact conservation. It is maintained
// unconditionally, not only under Verify, because the cost is a handful of
// integer adds per tick and a ledger that is only updated in verify builds
// would be the one piece of accounting nobody ever tests.
type tickLedger struct {
	regrown  int64 // food units added by regrowth
	eaten    int64 // food units removed by eating
	wasted   int64 // energy discarded because eating hit EnergyMax
	burnt    int64 // energy removed by metabolism
	diedWith int64 // energy removed along with dead agents
	births   int64 // births completed
}

func (l *tickLedger) reset() { *l = tickLedger{} }

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

	world.initBlockIndex()
	world.seedAgents()

	world.intents = make([]Intent, 0, len(world.Agents))
	world.births = make([]Birth, 0, 64)
	world.prevTotalEnergy = world.totalAgentEnergy()
	world.prevTotalFood = world.totalCellFood()

	if cfg.Verify {
		world.verifyErr = Verify(world)
	}

	return world
}

// seedAgents places the founding population on distinct cells with ages spread
// uniformly over [0, InitAgeSpread).
//
// The age spread is not cosmetic. Founders that all start at age 0 mature on
// the same tick and die on the same tick, producing a synchronised cohort that
// makes almost every run look OSCILLATING — an initialisation artefact
// masquerading as ecology.
//
// Ids are 1..InitAgents in order, so the strictly-ascending-id invariant holds
// from the first tick without any sorting.
func (w *World) seedAgents() {
	cellCount := int32(len(w.Cells))
	occupied := make([]bool, cellCount)

	for i := int32(0); i < w.Cfg.InitAgents; i++ {
		id := uint32(i) + 1

		position := AgentRand(w.Seed, id, 0, purposeSpawnPos)
		index := int32(position.Intn(uint32(cellCount)))

		// Linear probing keeps founders distinct and always terminates:
		// Validate guarantees InitAgents <= cellCount.
		for occupied[index] {
			index++
			if index == cellCount {
				index = 0
			}
		}
		occupied[index] = true

		age := AgentRand(w.Seed, id, 0, purposeInitAge)

		w.Agents = append(w.Agents, Agent{
			ID:            id,
			X:             w.cellX(index),
			Y:             w.cellY(index),
			Energy:        w.Cfg.InitAgentEnergy,
			Age:           int32(age.Intn(uint32(w.Cfg.InitAgeSpread))),
			LastBirthTick: -w.Cfg.BirthCooldown,
			TargetIdx:     -1,
		})
	}

	w.NextID = uint32(w.Cfg.InitAgents) + 1
}

func (w *World) totalAgentEnergy() int64 {
	total := int64(0)
	for i := range w.Agents {
		total += int64(w.Agents[i].Energy)
	}
	return total
}

func (w *World) totalCellFood() int64 {
	total := int64(0)
	for i := range w.Cells {
		total += int64(w.Cells[i].Food)
	}
	return total
}

// VerifyError returns the first invariant violation seen, or nil.
//
// It is always nil unless Config.Verify is set. Once a violation is recorded
// the simulation state is known bad, so Run stops rather than piling further
// assertions on top of a corrupt world.
func (w *World) VerifyError() error { return w.verifyErr }

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
