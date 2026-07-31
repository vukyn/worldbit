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
func Step(w *World) {
	cfg := w.Cfg
	w.ledger.reset()

	// ---- phase 1: environment (writes Cells) ----
	w.regrow()

	// ---- phase 2: decide (reads the world, writes only w.intents) ----
	if cap(w.intents) < len(w.Agents) {
		w.intents = make([]Intent, len(w.Agents), 2*len(w.Agents))
	}
	w.intents = w.intents[:len(w.Agents)]
	view := w.View()
	for k := range w.Agents {
		w.intents[k] = decide(view, &w.Agents[k])
	}

	// ---- phase 3: resolve (applies intents in ascending-id order) ----
	for k := range w.Agents {
		agent := &w.Agents[k]

		switch w.intents[k].Kind {
		case IntentMove:
			agent.X = w.intents[k].X
			agent.Y = w.intents[k].Y
			agent.TargetIdx = w.intents[k].Target

		case IntentEat:
			index := w.cellIdx(agent.X, agent.Y)
			// Re-checked against live state: a lower-id agent resolving
			// earlier this tick may already have taken this food.
			if w.Cells[index].Food > 0 {
				w.Cells[index].Food--
				w.blockFood[w.blockOf(index)]--

				gained := cfg.EnergyPerFood
				if agent.Energy+gained > cfg.EnergyMax {
					gained = cfg.EnergyMax - agent.Energy
				}
				agent.Energy += gained

				w.ledger.eaten++
				w.ledger.wasted += int64(cfg.EnergyPerFood - gained)
			}
			agent.TargetIdx = -1

		case IntentReproduce:
			// Re-checked for the same reason as eating: the decision was made
			// against the state at the start of the tick.
			if agent.Energy >= cfg.ReproEnergyMin {
				agent.Energy -= cfg.ReproEnergyCost
				agent.LastBirthTick = w.Tick
				// COORDINATES, never a slice index — phase 5 compacts the
				// agent slice in place and invalidates every index taken here.
				w.births = append(w.births, Birth{X: agent.X, Y: agent.Y})
			}

		case IntentIdle:
		}
	}

	// ---- phase 4: metabolism ----
	for k := range w.Agents {
		w.Agents[k].Energy -= cfg.BurnPerTick
		w.Agents[k].Age++
	}
	w.ledger.burnt = int64(cfg.BurnPerTick) * int64(len(w.Agents))

	// ---- phase 5: death (stable in-place compaction, preserves ascending id) ----
	keep := w.Agents[:0]
	for _, agent := range w.Agents {
		if agent.Energy > 0 && agent.Age <= cfg.MaxAge {
			keep = append(keep, agent)
			continue
		}
		w.ledger.diedWith += int64(agent.Energy)
	}
	w.Agents = keep

	// ---- phase 6: birth (parent order; new ids are the largest, so appending
	// preserves the ascending-id invariant without sorting) ----
	for _, birth := range w.births {
		w.Agents = append(w.Agents, Agent{
			ID:            w.NextID,
			X:             birth.X,
			Y:             birth.Y,
			Energy:        cfg.ChildEnergy,
			Age:           0,
			LastBirthTick: -cfg.BirthCooldown,
			TargetIdx:     -1,
		})
		w.NextID++
	}
	w.ledger.births = int64(len(w.births))
	w.births = w.births[:0]

	w.Tick++

	if cfg.Verify && w.verifyErr == nil {
		w.verifyErr = Verify(w)
	}
}

// Run advances the world to the given tick.
//
// It stops early if Verify is enabled and an invariant has been violated: past
// that point every subsequent tick is built on known-bad state, so continuing
// only buries the first failure.
func Run(w *World, untilTick int32) {
	for w.Tick < untilTick {
		Step(w)
		if w.verifyErr != nil {
			return
		}
	}
}
