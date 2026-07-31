package sim

import "errors"

// Verify asserts every structural and physical invariant of the world.
//
// It is gated by Config.Verify because it walks the whole grid and rebuilds the
// coarse index on every tick. Under --verify a run is roughly an order of
// magnitude slower and every tick is checked; without it, nothing here runs.
//
// The energy-conservation check at the end is the single strongest bug net in
// the simulation. Structural checks catch state that is obviously wrong;
// conservation catches state that is merely wrong — an eat that credits the
// wrong amount, a birth whose overhead goes missing, a death that quietly
// evaporates energy — on the exact tick it happens rather than three phases
// later when a population curve looks odd.
func Verify(w *World) error {
	if err := w.verifyAgents(); err != nil {
		return err
	}
	if err := w.verifyCells(); err != nil {
		return err
	}
	if err := w.verifyBlockIndex(); err != nil {
		return err
	}
	return w.verifyEnergyConservation()
}

// verifyAgents checks the agent-slice invariants the whole tick loop depends
// on. Ascending ids in particular are load-bearing: resolve order, death
// compaction and birth appending are all deterministic only because of them.
func (w *World) verifyAgents() error {
	cfg := w.Cfg
	cellCount := int32(len(w.Cells))

	previousID := uint32(0)
	for k := range w.Agents {
		agent := &w.Agents[k]

		if k > 0 && agent.ID <= previousID {
			return failure("agent order: index ", int64(k), " has id ", int64(agent.ID),
				" which does not exceed the previous id ", int64(previousID),
				" — the ascending-id invariant is what makes resolve, death and birth deterministic")
		}
		previousID = agent.ID

		if agent.ID >= w.NextID {
			return failure("agent ", int64(agent.ID), ": id is at or above NextID ", int64(w.NextID),
				" — ids must never be reused")
		}
		if agent.Energy < 1 || agent.Energy > cfg.EnergyMax {
			return failure("agent ", int64(agent.ID), ": energy ", int64(agent.Energy),
				" outside [1, ", int64(cfg.EnergyMax), "]")
		}
		if agent.Age < 0 || agent.Age > cfg.MaxAge {
			return failure("agent ", int64(agent.ID), ": age ", int64(agent.Age),
				" outside [0, ", int64(cfg.MaxAge), "]")
		}
		if int32(agent.X) >= cfg.Width || int32(agent.Y) >= cfg.Height {
			return failure("agent ", int64(agent.ID), ": position ", int64(agent.X), ",", int64(agent.Y),
				" is off the ", int64(cfg.Width), "x", int64(cfg.Height), " grid")
		}
		if agent.TargetIdx < -1 || agent.TargetIdx >= cellCount {
			return failure("agent ", int64(agent.ID), ": target ", int64(agent.TargetIdx),
				" is neither -1 nor a cell index below ", int64(cellCount))
		}
		if agent.LastBirthTick > w.Tick {
			return failure("agent ", int64(agent.ID), ": last birth tick ", int64(agent.LastBirthTick),
				" is in the future (now ", int64(w.Tick), ")")
		}
	}

	return nil
}

func (w *World) verifyCells() error {
	foodMax := w.Cfg.FoodMax
	for index := range w.Cells {
		food := w.Cells[index].Food
		if food < 0 || food > foodMax {
			return failure("cell ", int64(index), ": food ", int64(food),
				" outside [0, ", int64(foodMax), "]")
		}
	}
	return nil
}

// verifyBlockIndex rebuilds the coarse food index from scratch and compares.
//
// This is why blockFood can safely stay out of the canonical hash: hashing a
// derived cache would pin it into the golden fixtures and make any legitimate
// change to the index brittle, whereas rebuild-and-compare proves the stronger
// property that the cache agrees with the state it summarises.
func (w *World) verifyBlockIndex() error {
	if len(w.blockScratch) != len(w.blockFood) {
		w.blockScratch = make([]int32, len(w.blockFood))
	}
	w.rebuildBlockIndex(w.blockScratch)

	for block := range w.blockFood {
		if w.blockFood[block] != w.blockScratch[block] {
			return failure("block index: block ", int64(block), " holds ", int64(w.blockFood[block]),
				" but the cells in it total ", int64(w.blockScratch[block]),
				" — incremental maintenance on eat or regrow is out of step")
		}
	}
	return nil
}

// verifyEnergyConservation asserts that this tick's change in world food and in
// agent energy is exactly explained by the sources and sinks the tick recorded.
//
// The food and energy sides are checked separately before the combined
// identity, because a combined-only check tells you that something is wrong
// while the split check tells you WHICH side and by how much — the difference
// between a minute and an afternoon.
//
// Food:    ΔF = regrown − eaten
// Energy:  ΔE = (EnergyPerFood·eaten − wasted) − burnt − diedWith − births·(ReproEnergyCost − ChildEnergy)
// Combined (the plan's statement, implied by the two above):
//
//	ΔE + EnergyPerFood·ΔF = EnergyPerFood·regrown − burnt − diedWith − birthOverhead − wasted
func (w *World) verifyEnergyConservation() error {
	// Tick 0 has no preceding tick to conserve against; the structural checks
	// above still apply to the initial state.
	if w.Tick == 0 {
		w.prevTotalEnergy = w.totalAgentEnergy()
		w.prevTotalFood = w.totalCellFood()
		return nil
	}

	cfg := w.Cfg
	energyPerFood := int64(cfg.EnergyPerFood)
	birthOverhead := w.ledger.births * int64(cfg.ReproEnergyCost-cfg.ChildEnergy)

	totalEnergy := w.totalAgentEnergy()
	totalFood := w.totalCellFood()

	deltaEnergy := totalEnergy - w.prevTotalEnergy
	deltaFood := totalFood - w.prevTotalFood

	// Advance the baseline before returning either way: a run that keeps going
	// after a reported violation should compare against real state, not a
	// baseline frozen at the moment of the first failure.
	w.prevTotalEnergy = totalEnergy
	w.prevTotalFood = totalFood

	expectedDeltaFood := w.ledger.regrown - w.ledger.eaten
	if deltaFood != expectedDeltaFood {
		return failure("tick ", int64(w.Tick), ": FOOD conservation off by ",
			deltaFood-expectedDeltaFood,
			" — world food moved by ", deltaFood,
			" but the tick recorded regrown ", w.ledger.regrown,
			" and eaten ", w.ledger.eaten, ", predicting ", expectedDeltaFood)
	}

	eatenIn := energyPerFood*w.ledger.eaten - w.ledger.wasted
	expectedDeltaEnergy := eatenIn - w.ledger.burnt - w.ledger.diedWith - birthOverhead
	if deltaEnergy != expectedDeltaEnergy {
		return failure("tick ", int64(w.Tick), ": ENERGY conservation off by ",
			deltaEnergy-expectedDeltaEnergy,
			" — agent energy moved by ", deltaEnergy, " but the tick recorded",
			" eating +", eatenIn,
			" (", w.ledger.eaten, " food, ", w.ledger.wasted, " wasted above the cap),",
			" burn -", w.ledger.burnt,
			", death -", w.ledger.diedWith,
			", birth overhead -", birthOverhead,
			" (", w.ledger.births, " births), predicting ", expectedDeltaEnergy)
	}

	observed := deltaEnergy + energyPerFood*deltaFood
	expected := energyPerFood*w.ledger.regrown - w.ledger.burnt - w.ledger.diedWith -
		birthOverhead - w.ledger.wasted
	if observed != expected {
		return failure("tick ", int64(w.Tick), ": COMBINED conservation off by ", observed-expected,
			" — ΔE + ", energyPerFood, "·ΔF = ", observed,
			" but regrowth in ", energyPerFood*w.ledger.regrown,
			" minus burn ", w.ledger.burnt,
			", death ", w.ledger.diedWith,
			", birth overhead ", birthOverhead,
			" and over-cap waste ", w.ledger.wasted, " predicts ", expected)
	}

	return nil
}

// failure builds an invariant-violation error out of alternating string and
// int64 fragments.
//
// internal/sim may not import fmt or strconv — the determinism allowlist is
// exactly math/bits, encoding/binary, sort and errors — so number formatting is
// spelled out here rather than smuggled in by widening the allowlist for the
// sake of an error message.
func failure(parts ...any) error {
	buffer := make([]byte, 0, 256)

	for _, part := range parts {
		switch typed := part.(type) {
		case string:
			buffer = append(buffer, typed...)
		case int64:
			buffer = appendInt(buffer, typed)
		}
	}

	return errors.New(string(buffer))
}

func appendInt(buffer []byte, value int64) []byte {
	if value < 0 {
		buffer = append(buffer, '-')
		// Negating math.MinInt64 overflows, so the magnitude is taken in the
		// unsigned domain.
		return appendUint(buffer, uint64(-(value+1))+1)
	}
	return appendUint(buffer, uint64(value))
}

func appendUint(buffer []byte, value uint64) []byte {
	var digits [20]byte

	position := len(digits)
	for {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
		if value == 0 {
			break
		}
	}

	return append(buffer, digits[position:]...)
}
