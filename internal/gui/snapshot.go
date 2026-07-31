//go:build !nogui

package gui

import "github.com/vukyn/worldbit/internal/sim"

const (
	// snapshotStride is how many ticks pass between clones. Twelve clones of a
	// default run is about a megabyte, which buys a backward scrub that costs
	// at most a thousand ticks of re-simulation — a few tens of milliseconds —
	// instead of a replay from tick zero.
	snapshotStride = 1000

	// maxSnapshots caps the memory a deep run may spend on scrubbing. A
	// 120 000-tick run would otherwise hold 120 clones; instead the stride
	// widens and the worst-case re-step gets longer, which is the right thing
	// to trade away.
	maxSnapshots = 64
)

// snapshotRing holds periodic clones of the world so backward scrubbing does
// not have to replay from the seed every time.
//
// It is a pure optimisation over something the seed can always reproduce: it is
// never written to disk, never serialised, and never part of any persisted
// format. Snapshots are addressed by tick rather than kept in insertion order,
// so scrubbing back and forth over the same stretch reuses the clones already
// taken instead of thrashing them.
type snapshotRing struct {
	stride int32
	slots  []*sim.World
}

func newSnapshotRing(maxTick int32) *snapshotRing {
	stride := int32(snapshotStride)
	for maxTick/stride > maxSnapshots {
		stride *= 2
	}

	slots := int(maxTick/stride) + 1
	return &snapshotRing{stride: stride, slots: make([]*sim.World, slots)}
}

// capture clones the world if it has just landed on a stride boundary that has
// no snapshot yet.
//
// Re-stepping over a stretch that was already captured finds the slot occupied
// and does nothing, which is correct as well as cheap: determinism guarantees
// the state at a given tick is the state that was there before.
func (r *snapshotRing) capture(world *sim.World) {
	if world.Tick <= 0 || world.Tick%r.stride != 0 {
		return
	}

	slot := int(world.Tick / r.stride)
	if slot >= len(r.slots) || r.slots[slot] != nil {
		return
	}
	r.slots[slot] = world.Clone()
}

// nearest returns the newest snapshot at or before tick, or nil when there is
// none and the caller must start from the seed.
//
// The returned world belongs to the ring; a caller that intends to step it must
// clone it first.
func (r *snapshotRing) nearest(tick int32) *sim.World {
	if tick < 0 || len(r.slots) == 0 {
		return nil
	}

	slot := int(tick / r.stride)
	if slot >= len(r.slots) {
		slot = len(r.slots) - 1
	}
	for ; slot >= 0; slot-- {
		if candidate := r.slots[slot]; candidate != nil && candidate.Tick <= tick {
			return candidate
		}
	}
	return nil
}
