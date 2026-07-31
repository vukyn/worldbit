package sim

// maxRingRadius bounds the precomputed Chebyshev ring offset table.
//
// 64 is half the default grid dimension: on a torus a ring wider than that
// wraps onto itself and revisits cells, so a larger search radius would not
// mean "look further", it would mean "look at the same cells twice".
// Config.Validate rejects a SearchRadius above this, so the table lookup in
// findNearestFood needs no bounds branch.
const maxRingRadius = 64

// ringOffset is one cell offset on a Chebyshev ring. int8 is sufficient because
// the offsets are bounded by maxRingRadius, and it keeps the whole table inside
// a few CPU cache lines per ring.
type ringOffset struct {
	dx, dy int8
}

// ringOffsets[r] is the ring of cells at exactly Chebyshev distance r from the
// origin, in a fixed order. Built once at process start: the table is the same
// for every world, every seed and every run, so it is pure shared read-only
// data and cannot leak state between simulations.
var ringOffsets [maxRingRadius + 1][]ringOffset

// wanderDirections is the 8-neighbourhood in a fixed order, starting north and
// turning clockwise. The order is part of the determinism contract: changing it
// changes every hash.
var wanderDirections = [8][2]int32{
	{0, -1}, {1, -1}, {1, 0}, {1, 1},
	{0, 1}, {-1, 1}, {-1, 0}, {-1, -1},
}

func init() {
	for radius := 1; radius <= maxRingRadius; radius++ {
		ring := make([]ringOffset, 0, 8*radius)

		// Clockwise from the top-left corner: top edge, right edge, bottom
		// edge, left edge. Corners belong to the horizontal edges only, so
		// every ring cell appears exactly once and len(ring) == 8*radius.
		for dx := -radius; dx <= radius; dx++ {
			ring = append(ring, ringOffset{dx: int8(dx), dy: int8(-radius)})
		}
		for dy := -radius + 1; dy <= radius-1; dy++ {
			ring = append(ring, ringOffset{dx: int8(radius), dy: int8(dy)})
		}
		for dx := radius; dx >= -radius; dx-- {
			ring = append(ring, ringOffset{dx: int8(dx), dy: int8(radius)})
		}
		for dy := radius - 1; dy >= -radius+1; dy-- {
			ring = append(ring, ringOffset{dx: int8(-radius), dy: int8(dy)})
		}

		ringOffsets[radius] = ring
	}
}

// decide is the whole of an agent's behaviour: one pure function from a
// read-only world view plus the agent's own state to a single intent.
//
// It receives a WorldView rather than a *World, so "the decide phase cannot
// write" is close to compiler-enforced rather than merely a convention.
//
// Resolution order is deliberate. Reproduction outranks eating because an agent
// rich enough to breed is by definition not hungry; eating outranks moving
// because a hungry agent standing on food has nothing better to do.
func decide(view WorldView, agent *Agent) Intent {
	cfg := view.Cfg()

	mature := agent.Age >= cfg.MatureAge
	offCooldown := view.Tick()-agent.LastBirthTick >= cfg.BirthCooldown
	if mature && offCooldown && agent.Energy >= cfg.ReproEnergyMin {
		return Intent{Kind: IntentReproduce, X: agent.X, Y: agent.Y, Target: agent.TargetIdx}
	}

	here := view.CellIdx(agent.X, agent.Y)
	if agent.Energy < cfg.HungerThreshold && view.Food(here) > 0 {
		return Intent{Kind: IntentEat, X: agent.X, Y: agent.Y, Target: -1}
	}

	// A cached target is followed only while it still holds food; another
	// agent may have eaten it since the target was chosen.
	if agent.TargetIdx >= 0 && view.Food(agent.TargetIdx) > 0 {
		x, y := view.stepToward(agent.X, agent.Y, agent.TargetIdx)
		return Intent{Kind: IntentMove, X: x, Y: y, Target: agent.TargetIdx}
	}

	if target := view.findNearestFood(agent); target >= 0 {
		x, y := view.stepToward(agent.X, agent.Y, target)
		return Intent{Kind: IntentMove, X: x, Y: y, Target: target}
	}

	x, y := view.wanderStep(agent)
	return Intent{Kind: IntentMove, X: x, Y: y, Target: -1}
}

// stepToward returns the coordinates one cell closer to the target, moving
// diagonally when that reduces the Chebyshev distance on both axes.
func (v WorldView) stepToward(x, y uint8, target int32) (uint8, uint8) {
	world := v.world

	dx := torusStep(int32(world.cellX(target))-int32(x), world.Cfg.Width)
	dy := torusStep(int32(world.cellY(target))-int32(y), world.Cfg.Height)

	return world.wrapX(int32(x) + dx), world.wrapY(int32(y) + dy)
}

// torusStep is the sign of the shortest wrapped path along one axis: -1, 0 or
// +1. size is a power of two, so the wrap is a mask.
//
// An exactly equidistant pair (the target is half the world away) resolves to
// +1. That is an arbitrary but fixed priority: what matters is that the tie
// never depends on anything except the two coordinates.
func torusStep(delta int32, size int32) int32 {
	wrapped := delta & (size - 1)
	switch {
	case wrapped == 0:
		return 0
	case wrapped*2 <= size:
		return 1
	default:
		return -1
	}
}

// wanderStep picks one of the eight neighbours at random.
//
// The draw comes from the agent's own (seed, id, tick, purpose) substream, so
// an agent's wander is unaffected by how many other agents exist or in what
// order they decided.
func (v WorldView) wanderStep(agent *Agent) (uint8, uint8) {
	world := v.world

	random := AgentRand(world.Seed, agent.ID, world.Tick, purposeWander)
	direction := wanderDirections[random.Intn(uint32(len(wanderDirections)))]

	return world.wrapX(int32(agent.X) + direction[0]), world.wrapY(int32(agent.Y) + direction[1])
}

// findNearestFood returns the index of a food-bearing cell within
// Cfg.SearchRadius, or -1.
//
// Two things keep this affordable. A whole-board scan would be 16384 cell reads
// per searching agent per tick — roughly 13 million reads a tick at carrying
// capacity, minutes per run:
//
//   - The coarse block index answers the famine case in nine reads. That is
//     exactly the case where every agent would otherwise pay the full scan on
//     every tick, so it is the fast path that matters most.
//   - Otherwise the search walks Chebyshev rings outward and stops at the first
//     ring containing food, which is typically a handful of cells.
//
// The intra-ring scan starts at a rotated index drawn from the agent's tiebreak
// substream. With a fixed start every agent would prefer the same compass
// direction on ties, and the population would visibly drift across the map —
// an artefact of the scan order, not of the ecology.
func (v WorldView) findNearestFood(agent *Agent) int32 {
	world := v.world

	if world.neighbourhoodEmpty(agent.X, agent.Y) {
		return -1
	}

	random := AgentRand(world.Seed, agent.ID, world.Tick, purposeTiebreak)
	originX := int32(agent.X)
	originY := int32(agent.Y)

	for radius := int32(1); radius <= world.Cfg.SearchRadius; radius++ {
		ring := ringOffsets[radius]
		ringLength := int32(len(ring))

		position := int32(random.Intn(uint32(ringLength)))
		for scanned := int32(0); scanned < ringLength; scanned++ {
			offset := ring[position]
			position++
			if position == ringLength {
				position = 0
			}

			index := world.cellIdx(
				world.wrapX(originX+int32(offset.dx)),
				world.wrapY(originY+int32(offset.dy)),
			)
			cell := &world.Cells[index]
			if cell.Food > 0 && cell.Biome != BiomeWater {
				return index
			}
		}
	}

	return -1
}
