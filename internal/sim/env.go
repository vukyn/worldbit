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
func (w *World) regrow() {
	cellCount := int32(len(w.Cells))
	stride := w.Cfg.FoodRegrowTicks
	foodMax := w.Cfg.FoodMax

	for index := w.Tick % stride; index < cellCount; index += stride {
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
