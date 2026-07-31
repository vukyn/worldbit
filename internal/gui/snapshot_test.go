//go:build !nogui

package gui

import (
	"testing"

	"github.com/vukyn/worldbit/internal/sim"
)

// TestSnapshotRingBoundsItsMemory: the ring is an optimisation, so it must not
// be able to turn a deep run into a memory problem. A 120 000-tick run widens
// the stride instead of holding a hundred and twenty clones.
func TestSnapshotRingBoundsItsMemory(t *testing.T) {
	shallow := newSnapshotRing(12000)
	if shallow.stride != snapshotStride {
		t.Errorf("a default run uses stride %d, want %d", shallow.stride, snapshotStride)
	}
	if len(shallow.slots) != 13 {
		t.Errorf("a 12000-tick run has %d slots, want 13", len(shallow.slots))
	}

	deep := newSnapshotRing(1200000)
	if len(deep.slots) > maxSnapshots+1 {
		t.Errorf("a 1.2M-tick run has %d slots, more than the %d cap",
			len(deep.slots), maxSnapshots+1)
	}
	if deep.stride <= snapshotStride {
		t.Errorf("a deep run kept the default stride %d instead of widening it", deep.stride)
	}
}

// TestSnapshotRingFindsTheNearestEarlierState covers the lookup a rewind
// depends on: never a snapshot from the future, always the closest one behind.
func TestSnapshotRingFindsTheNearestEarlierState(t *testing.T) {
	config := sim.DefaultConfig()
	config.Width = 32
	config.Height = 32
	config.InitAgents = 40
	config.MaxTick = 4000

	world := sim.NewWorld(3, config)
	ring := newSnapshotRing(config.MaxTick)
	for world.Tick < config.MaxTick {
		sim.Step(world)
		ring.capture(world)
	}

	if got := ring.nearest(500); got != nil {
		t.Errorf("a target before the first snapshot resolved to tick %d, want nothing", got.Tick)
	}
	for _, testCase := range []struct{ target, want int32 }{
		{target: 1000, want: 1000},
		{target: 1999, want: 1000},
		{target: 2000, want: 2000},
		{target: 4000, want: 4000},
		{target: 9999, want: 4000},
	} {
		snapshot := ring.nearest(testCase.target)
		if snapshot == nil {
			t.Fatalf("no snapshot at or before tick %d", testCase.target)
		}
		if snapshot.Tick != testCase.want {
			t.Errorf("nearest(%d) is tick %d, want %d",
				testCase.target, snapshot.Tick, testCase.want)
		}
	}
}

// TestSnapshotRingDoesNotOverwrite: re-stepping a stretch that was already
// captured must reuse the existing clones. Overwriting would be harmless
// (determinism guarantees they are equal) but it would make every scrub pay for
// a fresh deep copy of the grid.
func TestSnapshotRingDoesNotOverwrite(t *testing.T) {
	config := sim.DefaultConfig()
	config.Width = 16
	config.Height = 16
	config.InitAgents = 8

	world := sim.NewWorld(4, config)
	ring := newSnapshotRing(3000)
	for world.Tick < 2000 {
		sim.Step(world)
		ring.capture(world)
	}

	first := ring.nearest(1000)
	if first == nil {
		t.Fatal("nothing captured at tick 1000")
	}

	replay := sim.NewWorld(4, config)
	for replay.Tick < 1200 {
		sim.Step(replay)
		ring.capture(replay)
	}

	if again := ring.nearest(1000); again != first {
		t.Error("re-stepping replaced an existing snapshot instead of keeping it")
	}
}

// TestSnapshotRingIgnoresTickZero: tick zero is always reachable from the seed,
// so spending a clone on it would be pure waste.
func TestSnapshotRingIgnoresTickZero(t *testing.T) {
	config := sim.DefaultConfig()
	config.Width = 16
	config.Height = 16
	config.InitAgents = 8

	ring := newSnapshotRing(3000)
	ring.capture(sim.NewWorld(1, config))

	if got := ring.nearest(0); got != nil {
		t.Errorf("tick zero was captured (snapshot at tick %d)", got.Tick)
	}
}
