//go:build !nogui

package gui

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/vukyn/worldbit/internal/sim"
)

const (
	// minWindowWidth keeps the HUD legible when a small --scale would give the
	// grid less width than the text needs.
	//
	// It is exactly the width of the default 128-cell grid at the default scale
	// of 4, so the common case has no dead space beside the board; the longest
	// line the HUD draws is the key bindings, at about 480 pixels.
	minWindowWidth = 512

	// hudHeight is the panel below the grid: three lines of numbers, the
	// sparkline, the replay verdict and one line of key bindings.
	hudHeight = 154

	textLineHeight = 16
	bannerHeight   = 22

	sparklineTop    = 60
	sparklineHeight = 50
	verdictTop      = 114
	helpTop         = 132
)

// textLayer prints tinted text.
//
// ebitenutil.DebugPrintAt is the plan's deliberate choice for the first cut —
// zero font dependency, no asset to embed, no text/v2 face to configure — but
// it only draws white, and a mismatch warning that looks exactly like the tick
// counter is not a warning. So the text is drawn once into a scratch image and
// blitted through a colour scale. The scratch image is a single reused line, so
// this costs one extra draw call per line rather than one per frame.
//
// Swapping in a real text/v2 face later means changing this one type.
type textLayer struct {
	scratch *ebiten.Image
}

func newTextLayer(width int) *textLayer {
	return &textLayer{scratch: ebiten.NewImage(width, textLineHeight+2)}
}

func (t *textLayer) print(dst *ebiten.Image, message string, x, y int, tint color.Color) {
	t.scratch.Clear()
	ebitenutil.DebugPrintAt(t.scratch, message, 0, 0)

	options := &ebiten.DrawImageOptions{}
	options.GeoM.Translate(float64(x), float64(y))
	options.ColorScale.ScaleWithColor(tint)
	dst.DrawImage(t.scratch, options)
}

// drawHUD paints the panel under the grid and, when a replay has diverged, the
// banner over it.
func (g *game) drawHUD(screen *ebiten.Image) {
	if g.text == nil {
		g.text = newTextLayer(g.width)
	}

	top := g.gridHeight
	vector.DrawFilledRect(screen, 0, float32(top), float32(g.width), float32(hudHeight), colourPanel, false)

	// The hash is recomputed per displayed frame rather than per tick: it is a
	// walk of the grid, tens of microseconds, and at sixty frames a second that
	// is a rounding error next to the ticks themselves.
	hash := sim.Hash(g.world)
	peak := g.recorder.PeakPopulation()

	line := func(index int) int { return top + 6 + index*textLineHeight }

	g.text.print(screen, fmt.Sprintf("tick %6d / %-6d   year %4d   pop %5d   peak %5d @ y%d",
		g.world.Tick, g.options.Config.MaxTick, g.world.Year(),
		g.recorder.FinalPopulation(), peak, g.recorder.PeakYear()),
		8, line(0), colourText)

	g.text.print(screen, fmt.Sprintf("outcome %-12s hash %016x", g.recorder.Outcome(), hash),
		8, line(1), colourText)

	g.drawStatusLine(screen, 8, line(2), peak)

	drawSparkline(screen,
		g.recorder.History(),
		rect{x: 8, y: top + sparklineTop, width: g.width - 16, height: sparklineHeight},
		peak, colourSpark, colourSparkAxis)

	g.drawVerdict(screen, top, hash)

	g.text.print(screen,
		"space pause · 1-4 speed · arrows scrub (shift x10) · r restart · s seed · q quit",
		8, top+helpTop, colourTextDim)
}

// drawStatusLine reports the controls' current state, or the seed being typed.
func (g *game) drawStatusLine(screen *ebiten.Image, x, y, peak int) {
	if g.entryActive {
		g.text.print(screen,
			fmt.Sprintf("seed> %s_    enter to start, esc to cancel", g.entryDigits),
			x, y, colourGood)
		return
	}

	scale := peak
	if scale < 1 {
		scale = 1
	}
	g.text.print(screen,
		fmt.Sprintf("speed %-7s seed %-12d sparkline 0..%d pop", g.speedLabel(), g.seed, scale),
		x, y, colourTextDim)
}

// drawVerdict reports the replay check.
//
// A mismatch gets a red banner across the world itself, not a line of text in
// the panel: it means this binary no longer reproduces a recorded run, which is
// the single most serious thing this project can discover about itself, and it
// must not be possible to overlook. A match gets one dimmed line, because a
// replay that works is the expected case and does not deserve to compete with
// the simulation for attention.
func (g *game) drawVerdict(screen *ebiten.Image, top int, hash uint64) {
	switch g.replayVerdict(hash) {
	case verdictMatch:
		g.text.print(screen,
			fmt.Sprintf("replay verified — hash matches the recorded %016x", g.options.ExpectHash),
			8, top+verdictTop, colourGood)

	case verdictMismatch:
		g.text.print(screen,
			fmt.Sprintf("HASH MISMATCH at tick %d — this build does not reproduce the recorded run",
				g.world.Tick),
			8, top+verdictTop, colourAlert)

		vector.DrawFilledRect(screen, 0, 0, float32(g.width), bannerHeight, colourAlert, false)
		g.text.print(screen,
			fmt.Sprintf("HASH MISMATCH   got %016x   want %016x", hash, g.options.ExpectHash),
			8, 3, color.White)

	case verdictPending:
		g.text.print(screen,
			fmt.Sprintf("replay check pending — will compare against %016x", g.options.ExpectHash),
			8, top+verdictTop, colourTextDim)

	case verdictNone:
		if g.complete() {
			g.text.print(screen,
				fmt.Sprintf("run complete — state hash %016x", hash),
				8, top+verdictTop, colourTextDim)
		}
	}
}
