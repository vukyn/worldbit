package sim

// Torus topology. Width and Height are powers of two (enforced by
// Config.Validate), so indexing is a shift-or and wrapping is a mask —
// branchless, no boundary special case, and no edge-clustering artefact.

// cellIdx maps torus coordinates to a cell index.
func (w *World) cellIdx(x, y uint8) int32 {
	return (int32(y) << w.xShift) | int32(x)
}

// cellX is the x coordinate of a cell index.
func (w *World) cellX(index int32) uint8 { return uint8(index & w.xMask) }

// cellY is the y coordinate of a cell index.
func (w *World) cellY(index int32) uint8 { return uint8((index >> w.xShift) & w.yMask) }

// wrapX wraps an unbounded x coordinate onto the torus.
func (w *World) wrapX(x int32) uint8 { return uint8(x & w.xMask) }

// wrapY wraps an unbounded y coordinate onto the torus.
func (w *World) wrapY(y int32) uint8 { return uint8(y & w.yMask) }

// regrow advances food by one stride step.
//
// Exactly ceil((N-offset)/FoodRegrowTicks) cells regrow this tick, and each
// cell regrows exactly once every FoodRegrowTicks ticks. This is deliberately
// NOT "every cell every FoodRegrowTicks ticks": a synchronised global pulse
// would stamp a FoodRegrowTicks-period sawtooth onto every run and the
// classifier would be measuring the pulse rather than the ecology.
//
// The stride walks POSITIONS IN regrowOrder, not cell indices. Striding over
// raw indices gives the same per-tick workload and the same once-per-period
// coverage, but it also makes regrowth time an affine function of position on
// the board: index runs along x first, so cells one apart in x regrow one tick
// apart and regrowth sweeps the grid as a travelling horizontal front. Food
// availability ends up strongly correlated along rows and the agents visibly
// band up behind the front. Walking a permutation keeps both properties the
// stride was chosen for and removes the spatial one nobody asked for.
func (w *World) regrow() {
	cellCount := int32(len(w.regrowOrder))
	stride := w.Cfg.FoodRegrowTicks
	foodMax := w.Cfg.FoodMax

	for position := w.Tick % stride; position < cellCount; position += stride {
		index := w.regrowOrder[position]

		cell := &w.Cells[index]
		if cell.Biome == BiomeWater {
			continue
		}
		if cell.Food < foodMax {
			cell.Food++
			w.blockFood[w.blockOf(index)]++
			w.ledger.regrown++
		}
	}
}

// buildRegrowOrder returns the regrowth permutation for a grid of cellCount
// cells: Fisher-Yates over the positional stream, so it is a pure function of
// the cell count alone.
//
// The shuffle is driven by a FIXED constant rather than by the world seed. The
// seed-derived alternative was measured and rejected. It was expected to add
// seed-to-seed variance, which this project has conspicuously lacked; it adds
// none at all. Over 1000 seeds at the default parameters the two are
// statistically identical — variance ratio 1.0028 on final population and
// 0.9882 on peak, both dead centre of the 0.876-1.141 null band for F(999,999)
// — and over the 1200-run sweep grid they differ only by ordinary seed noise.
// Neither is simpler than the other here: the grid size is a config parameter,
// so a fixed table still cannot be built once in init(), and the two versions
// differ by exactly which uint64 goes in on the line below.
//
// The tiebreak is testability. A constant permutation is THE permutation every
// run in the project's history will ever use, so TestRegrowthOrderIsSpatially-
// Incoherent pins the property for all of them; the seed-derived version could
// only ever sample one seed's draw and hope. Regrowth order is a schedule, not
// terrain — seed-to-seed environmental variation is what P6's biome generation
// is for — and holding the schedule fixed across seeds means a sweep varies
// agents against one environment instead of varying both at once.
//
// Cost is one pass at construction and 4 bytes per cell for the life of the
// world (64 KB on the default grid).
func buildRegrowOrder(cellCount int32) []int32 {
	order := make([]int32, cellCount)
	for i := range order {
		order[i] = int32(i)
	}

	// Agent ids start at 1, so id 0 cannot collide with an agent's substream.
	// gamma64 is reused as the stream's world-seed slot purely as a fixed
	// nothing-up-my-sleeve word; nothing about the golden ratio matters here.
	stream := AgentRand(gamma64, 0, 0, purposeRegrowOrder)
	for i := cellCount - 1; i > 0; i-- {
		j := int32(stream.Intn(uint32(i) + 1))
		order[i], order[j] = order[j], order[i]
	}

	return order
}
