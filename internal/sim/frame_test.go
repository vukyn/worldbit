package sim

import "testing"

// TestGUINeverMutates is the guarantee behind the read-only boundary: a full
// render pass may not change the world by so much as one bit.
//
// The hash is the right instrument because it digests exactly the canonical
// state, so a render that quietly normalised a food value or compacted the
// agent slice would show up here even though the rendered picture looked fine.
// The pass is repeated across ticks and at both ends of the population curve,
// because a bug that only reads out of bounds when the grid is crowded would
// slip past a single check on a freshly seeded world.
func TestGUINeverMutates(t *testing.T) {
	config := DefaultConfig()
	config.MaxTick = 600
	world := NewWorld(7, config)

	frame := &Frame{}

	for world.Tick <= config.MaxTick {
		before := Hash(world)
		beforeTick := world.Tick
		beforePopulation := len(world.Agents)

		// Twice: the first pass allocates the slices, the second reuses them,
		// and the reuse path is the one that clears and refills in place.
		world.RenderInto(frame)
		world.RenderInto(frame)

		if after := Hash(world); after != before {
			t.Fatalf("tick %d: render mutated the world, hash %016x -> %016x",
				beforeTick, before, after)
		}
		if world.Tick != beforeTick {
			t.Fatalf("render advanced the tick to %d", world.Tick)
		}
		if len(world.Agents) != beforePopulation {
			t.Fatalf("tick %d: render changed the population %d -> %d",
				beforeTick, beforePopulation, len(world.Agents))
		}

		if world.Tick == config.MaxTick {
			break
		}
		Step(world)
	}
}

// TestRenderIntoMatchesTheWorld checks the frame actually describes the world
// it was filled from. TestGUINeverMutates would happily pass on a RenderInto
// that wrote nothing at all.
func TestRenderIntoMatchesTheWorld(t *testing.T) {
	config := DefaultConfig()
	world := NewWorld(11, config)
	Run(world, 400)

	frame := &Frame{}
	world.RenderInto(frame)

	cellCount := len(world.Cells)
	if frame.Tick != world.Tick || frame.Year != world.Year() {
		t.Errorf("frame at tick %d year %d, world at tick %d year %d",
			frame.Tick, frame.Year, world.Tick, world.Year())
	}
	if int(frame.Pop) != len(world.Agents) {
		t.Errorf("frame population %d, world population %d", frame.Pop, len(world.Agents))
	}
	if frame.Width != config.Width || frame.Height != config.Height || frame.FoodMax != config.FoodMax {
		t.Errorf("frame grid %dx%d foodmax %d, config %dx%d foodmax %d",
			frame.Width, frame.Height, frame.FoodMax, config.Width, config.Height, config.FoodMax)
	}
	if len(frame.Biome) != cellCount || len(frame.Food) != cellCount || len(frame.AgentAt) != cellCount {
		t.Fatalf("frame slice lengths %d/%d/%d, want %d",
			len(frame.Biome), len(frame.Food), len(frame.AgentAt), cellCount)
	}

	for index := range world.Cells {
		if frame.Biome[index] != world.Cells[index].Biome {
			t.Fatalf("cell %d biome %d, want %d", index, frame.Biome[index], world.Cells[index].Biome)
		}
		if frame.Food[index] != world.Cells[index].Food {
			t.Fatalf("cell %d food %d, want %d", index, frame.Food[index], world.Cells[index].Food)
		}
	}

	occupancy := make([]int, cellCount)
	for i := range world.Agents {
		occupancy[world.cellIdx(world.Agents[i].X, world.Agents[i].Y)]++
	}
	for index, count := range occupancy {
		if count > 255 {
			count = 255
		}
		if int(frame.AgentAt[index]) != count {
			t.Fatalf("cell %d holds %d agents, frame says %d", index, count, frame.AgentAt[index])
		}
	}
}

// TestRenderIntoClearsStaleOccupancy is the specific bug the reuse path invites:
// a frame refilled without clearing AgentAt would keep painting agents on cells
// they have long since walked away from, and the picture would slowly fill up
// with ghosts while every other check still passed.
func TestRenderIntoClearsStaleOccupancy(t *testing.T) {
	config := DefaultConfig()
	world := NewWorld(3, config)

	frame := &Frame{}
	world.RenderInto(frame)

	Run(world, 300)
	world.RenderInto(frame)

	occupied := 0
	for _, count := range frame.AgentAt {
		if count > 0 {
			occupied += int(count)
		}
	}
	if occupied != len(world.Agents) {
		t.Fatalf("frame accounts for %d agents, world holds %d", occupied, len(world.Agents))
	}
}

// TestCloneIsIndependentAndReplaysIdentically is the whole basis of the scrub
// ring: a clone must both start equal and STAY equal as it is stepped, and
// stepping one copy must not disturb the other.
func TestCloneIsIndependentAndReplaysIdentically(t *testing.T) {
	config := DefaultConfig()
	world := NewWorld(19, config)
	Run(world, 250)

	clone := world.Clone()
	if got, want := Hash(clone), Hash(world); got != want {
		t.Fatalf("fresh clone hashes %016x, original %016x", got, want)
	}
	if clone.Tick != world.Tick {
		t.Fatalf("clone at tick %d, original at tick %d", clone.Tick, world.Tick)
	}

	for step := 0; step < 500; step++ {
		Step(world)
		Step(clone)
		if got, want := Hash(clone), Hash(world); got != want {
			t.Fatalf("tick %d: clone diverged, %016x != %016x", world.Tick, got, want)
		}
	}

	// The original must be untouched by further work on the clone.
	frozen := Hash(world)
	Run(clone, clone.Tick+100)
	if got := Hash(world); got != frozen {
		t.Fatalf("stepping the clone changed the original, %016x -> %016x", frozen, got)
	}
}

// TestCloneOfEmptyWorldSurvives covers the degenerate shape the scrub ring will
// meet on an extinct run: cloning a world with no agents at all must not trip
// over its zero-length slices.
func TestCloneOfEmptyWorldSurvives(t *testing.T) {
	config := DefaultConfig()
	config.InitAgents = 1
	config.MaxAge = 5
	world := NewWorld(2, config)
	Run(world, 50)

	if len(world.Agents) != 0 {
		t.Fatalf("expected an extinct world, got %d agents", len(world.Agents))
	}

	clone := world.Clone()
	if got, want := Hash(clone), Hash(world); got != want {
		t.Fatalf("clone of an empty world hashes %016x, original %016x", got, want)
	}
}
