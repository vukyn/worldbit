package sim

// Step advances the world by exactly one tick.
//
// Step takes no dt and never consults a clock: a tick is the unit of time.
// Wall-clock may decide HOW FAR the simulation has advanced when you look at
// it (the GUI's "max" speed), never WHAT state N contains.
//
// The tick is phase-separated: no phase both reads and writes the same layer.
// That is semantically identical to "read state N, write state N+1" without
// paying a full-world copy every tick.
//
// Phase order is part of the determinism contract. Changing the order — or the
// tiebreak scheme, or the RNG mixing — changes every hash and requires golden
// regeneration with a commit message that names the behavioural change.
//
// Phases 2 to 6 (decide, resolve, metabolism, death, birth) arrive with agents
// in the next phase of the project; the world currently has none.
func Step(w *World) {
	// ---- phase 1: environment (writes Cells) ----
	w.regrow()

	w.Tick++
}

// Run advances the world to the given tick.
func Run(w *World, untilTick int32) {
	for w.Tick < untilTick {
		Step(w)
	}
}
