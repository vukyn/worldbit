package sim

// An Intent is what one agent decided to do this tick, produced by the decide
// phase and applied by the resolve phase.
//
// Intents exist so that no phase both reads and writes the same layer: decide
// reads the world and writes only intents, resolve reads intents and writes the
// world. That is semantically identical to "read state N, write state N+1"
// without paying a full-world copy every tick.
//
// An intent is a decision, never a guarantee. Resolve re-checks anything that a
// lower-id agent could have invalidated in the same tick.
type IntentKind uint8

const (
	// IntentIdle does nothing. It is the zero value so a stale or unwritten
	// intent slot can never move an agent.
	IntentIdle IntentKind = iota
	// IntentMove walks one cell towards X, Y and adopts Target as the cached
	// food target.
	IntentMove
	// IntentEat consumes one food from the cell the agent is standing on,
	// subject to the food still being there at resolve time.
	IntentEat
	// IntentReproduce queues a birth at the parent's position.
	IntentReproduce
)

// Intent is a fixed-size value so the intent buffer is a flat slice reused
// across ticks with no allocation.
type Intent struct {
	Kind   IntentKind
	X, Y   uint8
	Target int32
}

// Birth is a queued newborn, recorded during resolve and materialised in the
// birth phase.
//
// It carries the parent's COORDINATES, never the parent's slice index: the
// death phase compacts the agent slice in place between the two, so any index
// captured during resolve is meaningless by the time the birth is applied.
type Birth struct {
	X, Y uint8
}
