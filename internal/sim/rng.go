package sim

// Per-agent RNG substreams.
//
// There is no shared sequential stream anywhere in the simulation. A shared
// stream couples every agent to every other agent's draw count, so adding or
// removing a single agent shifts the whole world's randomness — the classic
// way a reproducible sim quietly stops being reproducible.
//
// The only entry point is AgentRand, a pure function of four words. Same four
// inputs, same stream, forever.
const (
	purposeWander   = 1
	purposeTiebreak = 2
	purposeSpawnPos = 3
	purposeInitAge  = 4
	// purposeRegrowOrder is drawn once per world, not once per agent: it seeds
	// the Fisher-Yates shuffle behind the regrowth permutation. It is on the
	// same numbering as the agent purposes because it uses the same stream
	// machinery, with agent id 0 — an id no agent ever has.
	purposeRegrowOrder = 5
)

const (
	gamma64 = 0x9E3779B97F4A7C15
	mixA64  = 0xBF58476D1CE4E5B9
	mixB64  = 0x94D049BB133111EB
)

func splitmix64(x uint64) uint64 {
	x += gamma64
	z := x
	z = (z ^ (z >> 30)) * mixA64
	z = (z ^ (z >> 27)) * mixB64
	return z ^ (z >> 31)
}

// Rand is a positional stream. Its start state is a pure function of
// (worldSeed, agentID, tick, purpose), so streams are independent by
// construction rather than by discipline.
type Rand struct {
	state uint64
}

// AgentRand derives an agent's stream for one purpose on one tick.
//
// Multiple draws within a tick pull successively from the returned Rand; that
// stays pure because the start state depends only on the four inputs. Distinct
// purpose constants keep unrelated decisions uncorrelated.
func AgentRand(worldSeed uint64, agentID uint32, tick int32, purpose uint32) Rand {
	hash := splitmix64(worldSeed ^ (uint64(agentID) * gamma64))
	hash = splitmix64(hash ^ (uint64(uint32(tick)) * mixA64))
	hash = splitmix64(hash ^ (uint64(purpose) * mixB64))
	return Rand{state: hash}
}

// Next advances the stream and returns the next 64 random bits.
func (r *Rand) Next() uint64 {
	r.state = splitmix64(r.state)
	return r.state
}

// Intn returns an unbiased value in [0, n) using Lemire's multiply-shift with
// bounded rejection. Integer only — no floats, no division in the common path.
func (r *Rand) Intn(n uint32) uint32 {
	if n == 0 {
		return 0
	}

	candidate := uint32(r.Next() >> 32)
	product := uint64(candidate) * uint64(n)
	low := uint32(product)

	if low < n {
		threshold := (-n) % n // (2^32 - n) % n
		for low < threshold {
			candidate = uint32(r.Next() >> 32)
			product = uint64(candidate) * uint64(n)
			low = uint32(product)
		}
	}

	return uint32(product >> 32)
}
