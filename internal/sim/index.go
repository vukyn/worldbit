package sim

import "math/bits"

// Coarse food index.
//
// blockFood holds the total food of each 8x8 block of cells, so a 128x128 grid
// reduces to a 16x16 summary. Its only job is the famine fast path in
// findNearestFood: when an agent's block and its eight torus neighbours are all
// empty there is provably no food nearby and the fine ring scan — up to 625
// cell reads, per hungry agent, per tick — is skipped entirely.
//
// The index is DERIVED state. It is maintained incrementally by the two places
// that change food (regrowth adds, eating removes) and is deliberately NOT part
// of the canonical hash: hashing a derived cache would make the golden fixtures
// brittle to legitimate optimisation. It is guarded instead by
// rebuild-and-compare inside Verify, which is the strictly stronger check.
const blockShift = 3 // 8x8 cells per block

// initBlockIndex sizes the coarse index for the world's grid.
//
// Width and Height are powers of two (Config.Validate enforces it), so the
// block counts are powers of two too and block addressing stays a shift-or.
// Grids smaller than one block collapse to a single block, which is degenerate
// but correct: the fast path then simply never fires.
func (w *World) initBlockIndex() {
	w.blockCols = w.Cfg.Width >> blockShift
	if w.blockCols < 1 {
		w.blockCols = 1
	}
	w.blockRows = w.Cfg.Height >> blockShift
	if w.blockRows < 1 {
		w.blockRows = 1
	}
	w.blockColShift = uint(bits.TrailingZeros32(uint32(w.blockCols)))

	w.blockFood = make([]int32, w.blockCols*w.blockRows)
	w.rebuildBlockIndex(w.blockFood)
}

// blockOf maps a cell index to its block index.
func (w *World) blockOf(index int32) int32 {
	x := index & w.xMask
	y := (index >> w.xShift) & w.yMask
	return ((y >> blockShift) << w.blockColShift) | (x >> blockShift)
}

// rebuildBlockIndex recomputes the whole index from the cells.
//
// Used to populate the index once at world creation, and then only by Verify:
// the whole point of the incremental maintenance is that the running simulation
// never needs a rebuild. A rebuild appearing on a hot path is a bug.
func (w *World) rebuildBlockIndex(into []int32) {
	for i := range into {
		into[i] = 0
	}
	for index := range w.Cells {
		food := w.Cells[index].Food
		if food != 0 {
			into[w.blockOf(int32(index))] += int32(food)
		}
	}
}

// neighbourhoodEmpty reports whether the block containing (x, y) and its eight
// torus neighbours are all free of food.
//
// The nine blocks span 24x24 cells, and an agent sits at worst in a block
// corner, so a true answer proves there is no food within Chebyshev radius 8 of
// the agent. With SearchRadius above 8 the fast path is therefore a deliberate
// approximation: food at distance 9..12 in an otherwise-empty neighbourhood is
// not seen and the agent wanders instead. It is exact, deterministic and
// reproducible — the agent simply has a slightly smaller effective vision in
// near-famine, which is the regime where the saved 625-cell scan per agent per
// tick is the difference between a one-second run and a several-minute one.
func (w *World) neighbourhoodEmpty(x, y uint8) bool {
	blockX := int32(x) >> blockShift
	blockY := int32(y) >> blockShift
	colMask := w.blockCols - 1
	rowMask := w.blockRows - 1

	for dy := int32(-1); dy <= 1; dy++ {
		row := ((blockY + dy) & rowMask) << w.blockColShift
		for dx := int32(-1); dx <= 1; dx++ {
			if w.blockFood[row|((blockX+dx)&colMask)] != 0 {
				return false
			}
		}
	}
	return true
}
