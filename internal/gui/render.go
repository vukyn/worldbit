//go:build !nogui

package gui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/vukyn/worldbit/internal/sim"
)

// rgba is the paint-time colour type.
//
// It is image/color's RGBA in all but name, kept as a local alias-shaped struct
// so the inner loop over sixteen thousand cells indexes a flat array of four
// bytes rather than going through the color.Color interface once per pixel.
type rgba struct{ r, g, b, a uint8 }

func (c rgba) toColor() color.RGBA { return color.RGBA{R: c.r, G: c.g, B: c.b, A: c.a} }

// The palette. Bare ground is dark earth, food ramps to green, agents are a
// warm colour that no amount of vegetation can be confused with, and crowding
// pushes them towards white so a famine huddle is visibly different from a
// spread-out population.
var (
	colourWater      = rgba{28, 58, 108, 255}
	colourBare       = rgba{52, 40, 30, 255}
	colourLush       = rgba{58, 168, 76, 255}
	colourAgent      = rgba{255, 138, 46, 255}
	colourCrowd      = rgba{255, 244, 214, 255}
	colourBackground = color.RGBA{16, 18, 22, 255}
	colourPanel      = color.RGBA{24, 27, 33, 255}
	colourText       = color.RGBA{226, 232, 240, 255}
	colourTextDim    = color.RGBA{132, 142, 158, 255}
	colourGood       = color.RGBA{104, 176, 120, 255}
	colourAlert      = color.RGBA{198, 46, 46, 255}
	colourSpark      = color.RGBA{110, 190, 255, 255}
	colourSparkAxis  = color.RGBA{58, 66, 80, 255}
)

// crowdAt is the agent count at which the crowding tint is fully saturated.
const crowdAt = 6

// foodRamp precomputes the bare-to-lush gradient, one entry per food level.
//
// The ramp is built from the CONFIGURED maximum, not from the largest value on
// screen. A palette that rescaled itself each frame would paint a starving
// world and a lush one identically, which is precisely the difference the
// viewer exists to show.
func foodRamp(foodMax int16) []rgba {
	levels := int(foodMax) + 1
	if levels < 1 {
		levels = 1
	}

	ramp := make([]rgba, levels)
	for level := range ramp {
		ramp[level] = blend(colourBare, colourLush, level, levels-1)
	}
	return ramp
}

// agentColour tints an occupied cell by how many agents stand on it.
func agentColour(count uint8) rgba {
	return blend(colourAgent, colourCrowd, int(count)-1, crowdAt-1)
}

// blend interpolates from a to b at position/span, clamped at both ends. Span
// zero means there is nothing to interpolate over and a is the answer.
func blend(from, to rgba, position, span int) rgba {
	if span <= 0 || position <= 0 {
		return from
	}
	if position >= span {
		return to
	}

	mix := func(x, y uint8) uint8 {
		return uint8((int(x)*(span-position) + int(y)*position) / span)
	}
	return rgba{mix(from.r, to.r), mix(from.g, to.g), mix(from.b, to.b), 255}
}

// paintFrame writes one RGBA byte per channel per cell.
//
// Agents overwrite the terrain rather than blending with it: at one pixel per
// cell a translucent agent on lush ground is invisible, and knowing where the
// population is matters more than knowing what it is standing on.
//
// It reads the frame and nothing else — no world, no config — which is what
// makes the render path structurally incapable of touching the simulation.
func paintFrame(dst []byte, frame *sim.Frame, ramp []rgba) {
	for index := range frame.Biome {
		var paint rgba

		switch {
		case frame.AgentAt[index] > 0:
			paint = agentColour(frame.AgentAt[index])
		case frame.Biome[index] == sim.BiomeWater:
			paint = colourWater
		default:
			level := int(frame.Food[index])
			if level < 0 {
				level = 0
			}
			if level >= len(ramp) {
				level = len(ramp) - 1
			}
			paint = ramp[level]
		}

		offset := index * 4
		dst[offset] = paint.r
		dst[offset+1] = paint.g
		dst[offset+2] = paint.b
		dst[offset+3] = 255
	}
}

func (g *game) Draw(screen *ebiten.Image) {
	screen.Fill(colourBackground)

	// The single read of the simulation per displayed frame. Everything below
	// this line looks only at g.frame.
	g.world.RenderInto(g.frame)

	g.drawWorld(screen)
	g.drawHUD(screen)
}

// drawWorld paints the grid: one WritePixels into a canvas the size of the
// board, then one scaled DrawImage. Doing it the other way round — a draw call
// per cell — would be sixteen thousand draw calls a frame.
func (g *game) drawWorld(screen *ebiten.Image) {
	width, height := int(g.frame.Width), int(g.frame.Height)
	if width <= 0 || height <= 0 {
		return
	}

	if g.canvas == nil || g.canvas.Bounds().Dx() != width || g.canvas.Bounds().Dy() != height {
		g.canvas = ebiten.NewImage(width, height)
		g.pixels = make([]byte, width*height*4)
	}

	paintFrame(g.pixels, g.frame, g.ramp)
	g.canvas.WritePixels(g.pixels)

	options := &ebiten.DrawImageOptions{}
	options.GeoM.Scale(float64(g.options.Scale), float64(g.options.Scale))
	// Centred, because a scale small enough to make the window wider than the
	// board would otherwise leave the whole gutter on one side.
	options.GeoM.Translate(float64((g.width-g.gridWidth)/2), 0)
	// Nearest neighbour: a cell is a hard square of colour. Any smoothing
	// would invent food gradients that the simulation does not have.
	options.Filter = ebiten.FilterNearest
	screen.DrawImage(g.canvas, options)
}
