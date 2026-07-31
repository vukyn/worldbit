package sim

// Frame is the entire read path the viewer gets.
//
// The viewer never touches World fields. It asks for a Frame, and every pixel
// it paints comes from one of the slices below. That makes "the GUI mutates the
// simulation" structurally impossible rather than merely forbidden, and it is
// what TestGUINeverMutates pins.
//
// A Frame is allocated once and refilled: ~48 KB copied per rendered frame on
// the default grid, 2.9 MB/s at 60 fps, which is nothing next to the cost of
// the ticks themselves.
//
// Width, Height and FoodMax travel with the pixels deliberately. Without them
// the renderer would have to hold a Config alongside the Frame to know the grid
// shape and the top of the food ramp, and the read path would no longer be one
// self-contained value.
type Frame struct {
	// Tick, Year and Pop are the headline numbers the HUD reports.
	Tick int32
	Year int32
	Pop  int32

	// Width and Height describe the grid the three slices below index into:
	// every slice has Width*Height entries, index = (y*Width)+x.
	Width  int32
	Height int32

	// FoodMax is the top of the food ramp, so the renderer can scale colour
	// against the configured maximum rather than against whatever happens to
	// be the largest value on screen this frame — a rescaling palette would
	// make a starving world look identical to a lush one.
	FoodMax int16

	// Biome is the biome of every cell.
	Biome []uint8
	// Food is the food level of every cell, 0..FoodMax.
	Food []int16
	// AgentAt is how many agents stand on each cell, saturating at 255.
	// Zero means the cell is empty. It is a count rather than a flag because
	// crowding is invisible otherwise: a cell holding one agent and a cell
	// holding twelve are the same pixel, and the difference between a spread
	// population and a famine huddle is exactly what the viewer exists to show.
	AgentAt []uint8
}

// RenderInto fills f from the world, reusing f's slices when they already have
// the right length.
//
// It is a pure read of the world: nothing in this function writes through the
// receiver, which is the property TestGUINeverMutates asserts by hashing either
// side of a full render pass.
func (w *World) RenderInto(f *Frame) {
	cellCount := len(w.Cells)

	f.Tick = w.Tick
	f.Year = w.Year()
	f.Pop = int32(len(w.Agents))
	f.Width = w.Cfg.Width
	f.Height = w.Cfg.Height
	f.FoodMax = w.Cfg.FoodMax

	if len(f.Biome) != cellCount {
		f.Biome = make([]uint8, cellCount)
	}
	if len(f.Food) != cellCount {
		f.Food = make([]int16, cellCount)
	}
	if len(f.AgentAt) != cellCount {
		f.AgentAt = make([]uint8, cellCount)
	} else {
		clear(f.AgentAt)
	}

	for i := range w.Cells {
		f.Biome[i] = w.Cells[i].Biome
		f.Food[i] = w.Cells[i].Food
	}

	for i := range w.Agents {
		index := w.cellIdx(w.Agents[i].X, w.Agents[i].Y)
		if f.AgentAt[index] < 255 {
			f.AgentAt[index]++
		}
	}
}
