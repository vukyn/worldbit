package sim

// Canonical state hash: FNV-1a 64 over a little-endian encoding of the
// canonical state, hand-inlined. No hash.Hash64 interface, no allocation, no
// encoding/binary round trip — this is called at checkpoints on a ~66 KB
// working set and must not allocate.
//
// Only CANONICAL state is hashed. Derived caches (the coarse block-food index)
// are deliberately excluded: hashing a derived cache makes the golden fixtures
// brittle to legitimate optimisation. Derived state is guarded instead by
// rebuild-and-compare under Config.Verify.
const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

func mixByte(hash uint64, value uint8) uint64 {
	hash ^= uint64(value)
	hash *= fnvPrime64
	return hash
}

func mixBool(hash uint64, value bool) uint64 {
	if value {
		return mixByte(hash, 1)
	}
	return mixByte(hash, 0)
}

func mixUint16(hash uint64, value uint16) uint64 {
	hash = mixByte(hash, uint8(value))
	hash = mixByte(hash, uint8(value>>8))
	return hash
}

func mixUint32(hash uint64, value uint32) uint64 {
	hash = mixByte(hash, uint8(value))
	hash = mixByte(hash, uint8(value>>8))
	hash = mixByte(hash, uint8(value>>16))
	hash = mixByte(hash, uint8(value>>24))
	return hash
}

func mixInt16(hash uint64, value int16) uint64 { return mixUint16(hash, uint16(value)) }

func mixInt32(hash uint64, value int32) uint64 { return mixUint32(hash, uint32(value)) }

// Hash digests the canonical world state. Two worlds with the same hash at the
// same tick have byte-identical canonical state.
//
// Note the world Seed is deliberately NOT part of the digest: the hash answers
// "is this the same state", and the seed is an input recorded alongside it.
func Hash(w *World) uint64 {
	hash := uint64(fnvOffset64)

	hash = mixInt32(hash, w.Tick)
	hash = mixUint32(hash, w.NextID)
	hash = mixInt32(hash, int32(len(w.Agents)))

	for i := range w.Cells {
		hash = mixByte(hash, w.Cells[i].Biome)
		hash = mixInt16(hash, w.Cells[i].Food)
	}

	for i := range w.Agents {
		agent := &w.Agents[i]
		hash = mixUint32(hash, agent.ID)
		hash = mixByte(hash, agent.X)
		hash = mixByte(hash, agent.Y)
		hash = mixInt16(hash, agent.Energy)
		hash = mixInt32(hash, agent.Age)
		hash = mixInt32(hash, agent.LastBirthTick)
		hash = mixInt32(hash, agent.TargetIdx)
	}

	return hash
}
