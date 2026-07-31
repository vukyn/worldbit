//go:build !nogui

package gui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// rect is a widget's area in screen pixels.
type rect struct{ x, y, width, height int }

// drawSparkline paints a population history as a min/max envelope.
//
// The history is downsampled to the widget width by taking the range of each
// bucket rather than one sample from it. A twelve-thousand-tick run drawn into
// five hundred columns discards twenty-three out of every twenty-four samples
// if it picks one per column, and a boom-bust cycle sampled that way can come
// out looking almost flat. The envelope keeps the extremes, which are the whole
// point of looking at the curve.
//
// The full width is always used, so the curve stretches as the run grows rather
// than creeping in from the left; the tick counter in the HUD is what says how
// far along the run is.
func drawSparkline(dst *ebiten.Image, history []int, area rect, ceiling int, curve, axis color.Color) {
	vector.StrokeRect(dst,
		float32(area.x), float32(area.y), float32(area.width), float32(area.height),
		1, axis, false)

	columns := area.width - 2
	plotHeight := area.height - 2
	if len(history) == 0 || columns < 1 || plotHeight < 1 {
		return
	}
	if ceiling < 1 {
		ceiling = 1
	}

	baseline := float32(area.y + 1 + plotHeight)
	for column := 0; column < columns; column++ {
		low, high, ok := bucketRange(history, column, columns)
		if !ok {
			break
		}

		top := baseline - float32(scaled(high, ceiling, plotHeight))
		bottom := baseline - float32(scaled(low, ceiling, plotHeight))
		// A bucket whose samples are all equal has zero height and would draw
		// nothing at all, leaving gaps in a perfectly flat population.
		if bottom-top < 1 {
			bottom = top + 1
		}

		x := float32(area.x+1+column) + 0.5
		vector.StrokeLine(dst, x, top, x, bottom, 1, curve, false)
	}
}

// bucketRange is the lowest and highest sample in the slice of history that
// belongs to one column. ok is false once the columns outrun the samples.
func bucketRange(history []int, column, columns int) (int, int, bool) {
	start := column * len(history) / columns
	if start >= len(history) {
		return 0, 0, false
	}

	end := (column + 1) * len(history) / columns
	if end <= start {
		end = start + 1
	}

	low, high := history[start], history[start]
	for _, sample := range history[start+1 : end] {
		if sample < low {
			low = sample
		}
		if sample > high {
			high = sample
		}
	}
	return low, high, true
}

// scaled maps a population onto pixels of plot height, clamped to the top so a
// sample above the ceiling cannot draw outside the widget.
func scaled(value, ceiling, plotHeight int) int {
	if value <= 0 {
		return 0
	}
	pixels := value * plotHeight / ceiling
	if pixels > plotHeight {
		pixels = plotHeight
	}
	return pixels
}
