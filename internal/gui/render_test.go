//go:build !nogui

package gui

import (
	"testing"

	"github.com/vukyn/worldbit/internal/sim"
)

// TestPaintFrameNeverMutatesTheWorld is TestGUINeverMutates extended through
// the real paint path.
//
// The sim-side test proves RenderInto is a pure read; this one proves the code
// that consumes the frame is too, by driving the actual per-pixel loop the
// window uses and hashing the world either side of it. Only the ebiten draw
// calls are left out, and those take the byte slice this function fills.
func TestPaintFrameNeverMutatesTheWorld(t *testing.T) {
	config := sim.DefaultConfig()
	config.Width = 32
	config.Height = 32
	config.InitAgents = 40
	world := sim.NewWorld(5, config)
	sim.Run(world, 400)

	frame := &sim.Frame{}
	pixels := make([]byte, int(config.Width)*int(config.Height)*4)
	ramp := foodRamp(config.FoodMax)

	before := sim.Hash(world)
	world.RenderInto(frame)
	paintFrame(pixels, frame, ramp)
	world.RenderInto(frame)
	paintFrame(pixels, frame, ramp)

	if after := sim.Hash(world); after != before {
		t.Fatalf("the paint path mutated the world, hash %016x -> %016x", before, after)
	}
}

// TestPaintFramePaintsEveryCell guards the failure that a mutation test cannot
// see: a renderer that writes nothing, or writes transparent pixels, still
// leaves the world untouched.
func TestPaintFramePaintsEveryCell(t *testing.T) {
	config := sim.DefaultConfig()
	config.Width = 16
	config.Height = 16
	config.InitAgents = 8
	world := sim.NewWorld(2, config)
	sim.Run(world, 120)

	frame := &sim.Frame{}
	world.RenderInto(frame)

	cells := int(config.Width) * int(config.Height)
	pixels := make([]byte, cells*4)
	paintFrame(pixels, frame, foodRamp(config.FoodMax))

	agentPixels := 0
	for index := 0; index < cells; index++ {
		if pixels[index*4+3] != 255 {
			t.Fatalf("cell %d was painted with alpha %d", index, pixels[index*4+3])
		}
		if frame.AgentAt[index] > 0 {
			agentPixels++
			expected := agentColour(frame.AgentAt[index])
			if pixels[index*4] != expected.r || pixels[index*4+1] != expected.g {
				t.Fatalf("cell %d holds %d agents but was painted %d,%d,%d",
					index, frame.AgentAt[index],
					pixels[index*4], pixels[index*4+1], pixels[index*4+2])
			}
		}
	}

	if agentPixels == 0 {
		t.Fatal("no agent was painted; the test world is not exercising the agent path")
	}
}

// TestFoodRampSpansTheConfiguredRange pins the property that keeps a starving
// world visually distinct from a lush one: the ramp is anchored to FoodMax, so
// the colour of a given food level does not depend on what else is on screen.
func TestFoodRampSpansTheConfiguredRange(t *testing.T) {
	ramp := foodRamp(8)
	if len(ramp) != 9 {
		t.Fatalf("ramp for FoodMax 8 has %d entries, want 9", len(ramp))
	}
	if ramp[0] != colourBare {
		t.Errorf("empty ground is %v, want %v", ramp[0], colourBare)
	}
	if ramp[8] != colourLush {
		t.Errorf("full ground is %v, want %v", ramp[8], colourLush)
	}
	for level := 1; level < len(ramp); level++ {
		if ramp[level].g < ramp[level-1].g {
			t.Errorf("the ramp is not monotonic in green at level %d", level)
		}
	}

	// A degenerate FoodMax must still give a usable palette rather than an
	// empty slice the paint loop would index out of range.
	if got := len(foodRamp(0)); got != 1 {
		t.Errorf("ramp for FoodMax 0 has %d entries, want 1", got)
	}
}

// TestBucketRangeKeepsExtremes is the reason the sparkline downsamples by range
// rather than by picking one sample per column: a boom-bust cycle sampled at
// one point per column can come out looking flat.
func TestBucketRangeKeepsExtremes(t *testing.T) {
	history := []int{10, 200, 30, 5, 180, 40, 60, 120}

	low, high, ok := bucketRange(history, 0, 2)
	if !ok || low != 5 || high != 200 {
		t.Errorf("first half of %v is %d..%d (ok %v), want 5..200", history, low, high, ok)
	}
	low, high, ok = bucketRange(history, 1, 2)
	if !ok || low != 40 || high != 180 {
		t.Errorf("second half of %v is %d..%d (ok %v), want 40..180", history, low, high, ok)
	}

	// More columns than samples: every column must still resolve to a real
	// sample, so a short history stretches across the widget instead of
	// leaving holes in it.
	for column := 0; column < 8; column++ {
		if _, _, ok := bucketRange([]int{7, 9}, column, 8); !ok {
			t.Errorf("column %d of a two-sample history did not resolve", column)
		}
	}

	if _, _, ok := bucketRange(nil, 0, 8); ok {
		t.Error("an empty history resolved a column")
	}
}

// TestScaledClampsToThePlot: a sample above the ceiling must be flattened
// against the top of the widget rather than drawn outside it.
func TestScaledClampsToThePlot(t *testing.T) {
	if got := scaled(50, 100, 40); got != 20 {
		t.Errorf("half of a 40px plot is %d, want 20", got)
	}
	if got := scaled(400, 100, 40); got != 40 {
		t.Errorf("a sample above the ceiling scaled to %d, want 40", got)
	}
	if got := scaled(-5, 100, 40); got != 0 {
		t.Errorf("a negative sample scaled to %d, want 0", got)
	}
}
