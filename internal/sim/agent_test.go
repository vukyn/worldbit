package sim

import "testing"

// clearFood empties every cell and resyncs the derived index and the
// conservation baseline, so a test can build an exact scenario by hand.
func clearFood(w *World) {
	for i := range w.Cells {
		w.Cells[i].Food = 0
	}
	w.rebuildBlockIndex(w.blockFood)
	w.prevTotalFood = w.totalCellFood()
}

// replaceAgents installs a hand-built population and resyncs the derived state
// that NewWorld would otherwise have set.
func replaceAgents(w *World, agents []Agent, nextID uint32) {
	w.Agents = agents
	w.NextID = nextID
	w.prevTotalEnergy = w.totalAgentEnergy()
}

// chebyshev is the torus Chebyshev distance, used only as a test reference.
func chebyshev(ax, ay, bx, by int32, width, height int32) int32 {
	dx := (bx - ax) & (width - 1)
	if width-dx < dx {
		dx = width - dx
	}
	dy := (by - ay) & (height - 1)
	if height-dy < dy {
		dy = height - dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

// TestFoundersAreDistinctSpreadAndAscending pins the initial condition every
// golden hash is anchored to.
//
// The age spread carries the most weight. Founders all starting at age 0 mature
// together and die together, producing a synchronised cohort that makes nearly
// every run look OSCILLATING for reasons that are an initialisation artefact
// rather than ecology.
func TestFoundersAreDistinctSpreadAndAscending(t *testing.T) {
	cfg := DefaultConfig()
	world := NewWorld(11, cfg)

	if got := int32(len(world.Agents)); got != cfg.InitAgents {
		t.Fatalf("len(Agents) = %d, want %d", got, cfg.InitAgents)
	}
	if world.NextID != uint32(cfg.InitAgents)+1 {
		t.Errorf("NextID = %d, want %d", world.NextID, cfg.InitAgents+1)
	}

	occupied := make([]bool, len(world.Cells))
	distinctAges := 0
	seenAge := make([]bool, cfg.InitAgeSpread)

	for k := range world.Agents {
		agent := &world.Agents[k]

		if agent.ID != uint32(k)+1 {
			t.Fatalf("agent %d has id %d, want %d — founders must be in ascending id order", k, agent.ID, k+1)
		}
		if agent.Energy != cfg.InitAgentEnergy {
			t.Errorf("agent %d energy %d, want InitAgentEnergy %d", agent.ID, agent.Energy, cfg.InitAgentEnergy)
		}
		if agent.TargetIdx != -1 {
			t.Errorf("agent %d starts with target %d, want -1", agent.ID, agent.TargetIdx)
		}
		if agent.LastBirthTick != -cfg.BirthCooldown {
			t.Errorf("agent %d last birth tick %d, want %d", agent.ID, agent.LastBirthTick, -cfg.BirthCooldown)
		}
		if agent.Age < 0 || agent.Age >= cfg.InitAgeSpread {
			t.Fatalf("agent %d age %d outside [0, %d)", agent.ID, agent.Age, cfg.InitAgeSpread)
		}
		if !seenAge[agent.Age] {
			seenAge[agent.Age] = true
			distinctAges++
		}

		index := world.cellIdx(agent.X, agent.Y)
		if occupied[index] {
			t.Fatalf("agent %d shares cell %d with an earlier founder", agent.ID, index)
		}
		occupied[index] = true
	}

	// 50 founders over 240 ages: a synchronised cohort would collapse to one
	// or two distinct ages, so anything near the population size is proof the
	// spread is real rather than nominally present.
	if distinctAges < int(cfg.InitAgents)/2 {
		t.Errorf("founders have only %d distinct ages out of %d agents — the age spread is not spreading",
			distinctAges, cfg.InitAgents)
	}
}

// TestFoundersDifferPerSeed guards the other half of the initialisation: the
// founders must actually depend on the seed, or every run is the same run.
func TestFoundersDifferPerSeed(t *testing.T) {
	cfg := DefaultConfig()
	left := NewWorld(1, cfg)
	right := NewWorld(2, cfg)

	identical := true
	for k := range left.Agents {
		if left.Agents[k] != right.Agents[k] {
			identical = false
			break
		}
	}
	if identical {
		t.Error("seeds 1 and 2 produced identical founders — the spawn substream is not using the seed")
	}
}

// TestBirthUsesParentPositionNotSliceIndex is the regression test for the
// single most likely bug in the tick loop.
//
// Births are queued during resolve (phase 3) and applied after the death phase
// (phase 5) has compacted the agent slice in place. A birth that recorded its
// parent by slice index would, after compaction, read a different agent
// entirely — or run off the end of the slice. Recording coordinates is what
// makes the two phases independent.
//
// The scenario forces exactly that reshuffle: the agent at index 0 dies, so the
// reproducing parent moves from index 1 to index 0 between queueing the birth
// and the birth being applied.
func TestBirthUsesParentPositionNotSliceIndex(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Verify = true
	world := NewWorld(1, cfg)
	clearFood(world)

	const parentX, parentY = 100, 50

	replaceAgents(world, []Agent{
		// Index 0: starves this tick (energy 1, burn 1), so it is compacted
		// away in phase 5 and every later index shifts down by one.
		{ID: 1, X: 10, Y: 10, Energy: 1, Age: 0, LastBirthTick: -cfg.BirthCooldown, TargetIdx: -1},
		// Index 1: mature, off cooldown, rich enough to breed.
		{ID: 2, X: parentX, Y: parentY, Energy: cfg.EnergyMax, Age: cfg.MatureAge,
			LastBirthTick: -cfg.BirthCooldown, TargetIdx: -1},
	}, 3)

	Step(world)

	if err := world.VerifyError(); err != nil {
		t.Fatalf("invariants broken: %v", err)
	}
	if len(world.Agents) != 2 {
		t.Fatalf("population %d, want 2 (the starved founder gone, one newborn added)", len(world.Agents))
	}

	parent := world.Agents[0]
	child := world.Agents[1]

	if parent.ID != 2 {
		t.Fatalf("agent at index 0 is id %d, want the surviving parent id 2", parent.ID)
	}
	if child.ID != 3 {
		t.Fatalf("newborn id %d, want 3", child.ID)
	}
	if child.X != parentX || child.Y != parentY {
		t.Fatalf("newborn is at %d,%d but its parent is at %d,%d — the birth recorded a slice index, not a position",
			child.X, child.Y, parentX, parentY)
	}
	if child.Energy != cfg.ChildEnergy {
		t.Errorf("newborn energy %d, want ChildEnergy %d", child.Energy, cfg.ChildEnergy)
	}
	if child.Age != 0 || child.TargetIdx != -1 {
		t.Errorf("newborn age %d target %d, want 0 and -1", child.Age, child.TargetIdx)
	}
	// The parent pays the cost during resolve and burns during metabolism.
	if want := cfg.EnergyMax - cfg.ReproEnergyCost - cfg.BurnPerTick; parent.Energy != want {
		t.Errorf("parent energy %d, want %d", parent.Energy, want)
	}
	if parent.LastBirthTick != 0 {
		t.Errorf("parent last birth tick %d, want 0 (the tick the birth was queued)", parent.LastBirthTick)
	}
}

// TestEatIsRecheckedAtResolveTime pins the other same-tick hazard: two agents
// decide to eat the same food, and the lower id gets it.
//
// Both decisions were valid when they were made — decide reads the state at the
// start of the tick — so resolve must re-check against live state rather than
// trusting the intent. Without the re-check the cell would go to -1 food and
// twenty energy would appear from nowhere.
func TestEatIsRecheckedAtResolveTime(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Verify = true
	world := NewWorld(1, cfg)
	clearFood(world)

	// A cell the tick-0 regrowth stride does not touch, so the scenario is
	// exactly one food unit for two agents.
	const shared = int32(1291)
	if shared%cfg.FoodRegrowTicks == 0 {
		t.Fatalf("cell %d is on the tick-0 regrowth stride — pick another", shared)
	}
	world.Cells[shared].Food = 1
	world.blockFood[world.blockOf(shared)] = 1
	world.prevTotalFood = world.totalCellFood()

	x := world.cellX(shared)
	y := world.cellY(shared)
	const hungry = 50 // below HungerThreshold, so both intend to eat

	replaceAgents(world, []Agent{
		{ID: 1, X: x, Y: y, Energy: hungry, Age: 0, LastBirthTick: -cfg.BirthCooldown, TargetIdx: -1},
		{ID: 2, X: x, Y: y, Energy: hungry, Age: 0, LastBirthTick: -cfg.BirthCooldown, TargetIdx: -1},
	}, 3)

	Step(world)

	if err := world.VerifyError(); err != nil {
		t.Fatalf("invariants broken: %v", err)
	}
	if world.Cells[shared].Food != 0 {
		t.Fatalf("cell food %d after two agents ate one unit, want 0", world.Cells[shared].Food)
	}

	fed := int16(hungry + cfg.EnergyPerFood - cfg.BurnPerTick)
	starved := int16(hungry - cfg.BurnPerTick)
	if world.Agents[0].Energy != fed {
		t.Errorf("agent 1 (lowest id, resolves first) energy %d, want %d", world.Agents[0].Energy, fed)
	}
	if world.Agents[1].Energy != starved {
		t.Errorf("agent 2 energy %d, want %d — it ate food that was already gone",
			world.Agents[1].Energy, starved)
	}
}

// TestStepTowardTakesTheShortWayRound checks that movement crosses the seam
// rather than walking the long way round the torus, and that an exactly
// equidistant target resolves by a fixed rule rather than by accident.
func TestStepTowardTakesTheShortWayRound(t *testing.T) {
	cfg := DefaultConfig()
	world := NewWorld(1, cfg)
	view := world.View()

	cases := []struct {
		name           string
		fromX, fromY   uint8
		toX, toY       uint8
		wantX, wantY   uint8
		wantDescriptor string
	}{
		{"diagonal", 10, 10, 20, 20, 11, 11, "one step on both axes"},
		{"straight east", 10, 10, 20, 10, 11, 10, "x only"},
		{"already there", 10, 10, 10, 10, 10, 10, "no move"},
		{"west across the seam", 1, 10, 126, 10, 0, 10, "wraps below zero"},
		{"east across the seam", 126, 10, 1, 10, 127, 10, "wraps past the width"},
		{"north across the seam", 10, 0, 10, 126, 10, 127, "wraps below zero on y"},
		{"exactly half a world away", 10, 10, 74, 10, 11, 10, "fixed tiebreak: positive"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			target := world.cellIdx(testCase.toX, testCase.toY)
			gotX, gotY := view.stepToward(testCase.fromX, testCase.fromY, target)
			if gotX != testCase.wantX || gotY != testCase.wantY {
				t.Errorf("stepToward(%d,%d -> %d,%d) = %d,%d, want %d,%d (%s)",
					testCase.fromX, testCase.fromY, testCase.toX, testCase.toY,
					gotX, gotY, testCase.wantX, testCase.wantY, testCase.wantDescriptor)
			}
		})
	}
}

// TestRingTableIsExactChebyshevRings validates the precomputed offset table:
// every ring holds exactly the 8r cells at Chebyshev distance r, each once.
//
// A ring that double-counted a corner or skipped an edge cell would still
// produce a perfectly deterministic simulation, just a subtly biased one, so
// nothing else in the suite would ever notice.
func TestRingTableIsExactChebyshevRings(t *testing.T) {
	for radius := 1; radius <= maxRingRadius; radius++ {
		ring := ringOffsets[radius]

		if len(ring) != 8*radius {
			t.Fatalf("ring %d has %d cells, want %d", radius, len(ring), 8*radius)
		}

		width := 2*maxRingRadius + 1
		seen := make([]bool, width*width)
		for _, offset := range ring {
			dx := int(offset.dx)
			dy := int(offset.dy)

			distance := dx
			if distance < 0 {
				distance = -distance
			}
			absY := dy
			if absY < 0 {
				absY = -absY
			}
			if absY > distance {
				distance = absY
			}
			if distance != radius {
				t.Fatalf("ring %d contains offset %d,%d at Chebyshev distance %d", radius, dx, dy, distance)
			}

			slot := (dy+maxRingRadius)*width + (dx + maxRingRadius)
			if seen[slot] {
				t.Fatalf("ring %d contains offset %d,%d twice", radius, dx, dy)
			}
			seen[slot] = true
		}
	}
}

// TestFindNearestFoodReturnsAMinimalRingCell checks the bounded search against
// a brute-force reference over the whole search square: the returned cell must
// hold food and sit on the innermost ring that has any.
//
// The -1 case is checked against the block fast path's actual guarantee. Nine
// 8x8 blocks span 24x24 cells and the agent sits at worst in a block corner, so
// an empty neighbourhood proves there is no food within Chebyshev distance 8.
// Between 9 and SearchRadius the fast path is a deliberate approximation, which
// this test documents rather than forbids.
func TestFindNearestFoodReturnsAMinimalRingCell(t *testing.T) {
	cfg := DefaultConfig()
	world := NewWorld(99, cfg)
	view := world.View()

	// Deterministic scatter confined to one corner of the map: sparse enough
	// inside the patch that the search has to walk out several rings, and
	// empty enough outside it that the block fast path is exercised too.
	clearFood(world)
	scatter := AgentRand(99, 1, 0, purposeSpawnPos)
	for i := 0; i < 300; i++ {
		x := scatter.Intn(48)
		y := scatter.Intn(48)
		world.Cells[world.cellIdx(uint8(x), uint8(y))].Food = 1
	}
	world.rebuildBlockIndex(world.blockFood)

	const guaranteedRadius = 8
	found, empty := 0, 0

	for y := int32(0); y < cfg.Height; y += 3 {
		for x := int32(0); x < cfg.Width; x += 3 {
			agent := Agent{ID: uint32(y*cfg.Width + x + 1), X: uint8(x), Y: uint8(y), TargetIdx: -1}

			// Brute-force nearest ring holding food.
			bestRadius := int32(-1)
			for dy := -cfg.SearchRadius; dy <= cfg.SearchRadius; dy++ {
				for dx := -cfg.SearchRadius; dx <= cfg.SearchRadius; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					index := world.cellIdx(world.wrapX(x+dx), world.wrapY(y+dy))
					if world.Cells[index].Food <= 0 {
						continue
					}
					radius := chebyshev(x, y, int32(world.cellX(index)), int32(world.cellY(index)), cfg.Width, cfg.Height)
					if bestRadius < 0 || radius < bestRadius {
						bestRadius = radius
					}
				}
			}

			got := view.findNearestFood(&agent)

			if got < 0 {
				empty++
				if bestRadius >= 0 && bestRadius <= guaranteedRadius {
					t.Fatalf("agent at %d,%d found nothing, but food sits at Chebyshev distance %d, "+
						"inside the %d-cell radius the block fast path guarantees",
						x, y, bestRadius, guaranteedRadius)
				}
				continue
			}

			found++
			if world.Cells[got].Food <= 0 {
				t.Fatalf("agent at %d,%d targeted cell %d, which has no food", x, y, got)
			}
			radius := chebyshev(x, y, int32(world.cellX(got)), int32(world.cellY(got)), cfg.Width, cfg.Height)
			if radius != bestRadius {
				t.Fatalf("agent at %d,%d targeted a cell at Chebyshev distance %d, but the nearest food is at %d",
					x, y, radius, bestRadius)
			}
		}
	}

	if found == 0 || empty == 0 {
		t.Fatalf("scatter exercised only one branch (found %d, empty %d) — the test proves less than it looks",
			found, empty)
	}
}

// TestRingScanStartRotatesPerAgent is the anti-drift assertion.
//
// With food on every cell of the first ring, a fixed intra-ring scan order
// would make every agent pick the same compass direction, and the whole
// population would visibly drift across the map — an artefact of the scan
// order, not of the ecology. The rotated start index removes it while staying
// completely deterministic.
func TestRingScanStartRotatesPerAgent(t *testing.T) {
	cfg := DefaultConfig()
	world := NewWorld(5, cfg)
	view := world.View()

	clearFood(world)
	const centreX, centreY = 64, 64
	for _, offset := range ringOffsets[1] {
		index := world.cellIdx(
			world.wrapX(centreX+int32(offset.dx)),
			world.wrapY(centreY+int32(offset.dy)),
		)
		world.Cells[index].Food = 1
	}
	world.rebuildBlockIndex(world.blockFood)

	chosen := make([]int, len(ringOffsets[1]))
	for id := uint32(1); id <= 400; id++ {
		agent := Agent{ID: id, X: centreX, Y: centreY, TargetIdx: -1}
		target := view.findNearestFood(&agent)
		if target < 0 {
			t.Fatalf("agent %d found no food with all eight neighbours stocked", id)
		}

		for k, offset := range ringOffsets[1] {
			index := world.cellIdx(
				world.wrapX(centreX+int32(offset.dx)),
				world.wrapY(centreY+int32(offset.dy)),
			)
			if index == target {
				chosen[k]++
				break
			}
		}
	}

	for direction, count := range chosen {
		if count == 0 {
			t.Errorf("no agent ever chose ring direction %d — the intra-ring scan order is fixed, "+
				"which produces a collective drift artefact", direction)
		}
	}

	// Determinism is the other half of the property: the same agent on the
	// same tick must always choose the same cell.
	agent := Agent{ID: 7, X: centreX, Y: centreY, TargetIdx: -1}
	first := view.findNearestFood(&agent)
	for i := 0; i < 8; i++ {
		if got := view.findNearestFood(&agent); got != first {
			t.Fatalf("repeat %d of the same search returned %d, first returned %d", i, got, first)
		}
	}
}

// TestAgentIDsStayStrictlyAscending pins the invariant that lets resolve, death
// compaction and birth appending all be deterministic without any sorting.
func TestAgentIDsStayStrictlyAscending(t *testing.T) {
	world := NewWorld(3, DefaultConfig())

	for world.Tick < 3000 {
		Step(world)
		previous := uint32(0)
		for k := range world.Agents {
			if k > 0 && world.Agents[k].ID <= previous {
				t.Fatalf("tick %d: agent index %d has id %d, not above the previous id %d",
					world.Tick, k, world.Agents[k].ID, previous)
			}
			previous = world.Agents[k].ID
		}
	}
}

// TestVerifiedRunHoldsEveryTick is the P2 exit criterion as a test: a full
// 12000-tick run with every invariant checked on every tick.
func TestVerifiedRunHoldsEveryTick(t *testing.T) {
	if testing.Short() {
		t.Skip("12000 verified ticks")
	}

	for _, seed := range []uint64{1, 2, 3} {
		cfg := DefaultConfig()
		cfg.Verify = true

		world := NewWorld(seed, cfg)
		Run(world, cfg.MaxTick)

		if err := world.VerifyError(); err != nil {
			t.Fatalf("seed %d broke an invariant at tick %d: %v", seed, world.Tick, err)
		}
		if world.Tick != cfg.MaxTick {
			t.Fatalf("seed %d stopped at tick %d, want %d", seed, world.Tick, cfg.MaxTick)
		}
		if world.Population() == 0 {
			t.Errorf("seed %d went extinct — expected a surviving population near carrying capacity", seed)
		}
	}
}

// TestVerifyCatchesDeliberateCorruption proves the invariant checks are load
// bearing rather than decorative.
//
// Each case injects exactly the kind of damage a subtle bug in the tick loop
// would cause, and asserts Verify names it. Without this, a Verify that
// silently returned nil would let every other verified test pass forever.
func TestVerifyCatchesDeliberateCorruption(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(w *World)
		// stepAfter runs a full tick before checking, which is what the
		// conservation ledger needs: it compares the tick's recorded sources
		// and sinks against the state change they should have produced. The
		// structural checks are point-in-time, and some of the states they
		// catch (a target index off the grid) would panic the decide phase
		// long before a tick completed — which is exactly why they exist.
		stepAfter bool
	}{
		{"energy appearing from nowhere", func(w *World) { w.Agents[0].Energy += 7 }, true},
		{"energy quietly evaporating", func(w *World) { w.Agents[0].Energy -= 7 }, true},
		{"food appearing from nowhere", func(w *World) {
			w.Cells[4242].Food++
			w.blockFood[w.blockOf(4242)]++
		}, true},
		{"block index drifting out of step", func(w *World) { w.blockFood[0] += 3 }, false},
		{"agent ids out of order", func(w *World) {
			w.Agents[0].ID, w.Agents[1].ID = w.Agents[1].ID, w.Agents[0].ID
		}, false},
		{"energy above the cap", func(w *World) { w.Agents[0].Energy = w.Cfg.EnergyMax + 1 }, false},
		{"energy at zero on a living agent", func(w *World) { w.Agents[0].Energy = 0 }, false},
		{"food above the cap", func(w *World) { w.Cells[4242].Food = w.Cfg.FoodMax + 1 }, false},
		{"target index off the grid", func(w *World) { w.Agents[0].TargetIdx = int32(len(w.Cells)) }, false},
		{"age past MaxAge", func(w *World) { w.Agents[0].Age = w.Cfg.MaxAge + 1 }, false},
		{"an id at or above NextID", func(w *World) { w.Agents[len(w.Agents)-1].ID = w.NextID }, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Verify = true

			world := NewWorld(1, cfg)
			Run(world, 20)
			if err := world.VerifyError(); err != nil {
				t.Fatalf("the uncorrupted world already failed: %v", err)
			}

			testCase.corrupt(world)

			var caught error
			if testCase.stepAfter {
				Step(world)
				caught = world.VerifyError()
			} else {
				caught = Verify(world)
			}

			if caught == nil {
				t.Fatal("Verify accepted a deliberately corrupted world")
			}
			t.Logf("caught: %v", caught)
		})
	}
}

// TestEnergyConservationNamesTheOffendingSide checks the failure message, not
// just the failure. An assertion that fires with "something is wrong" costs an
// afternoon; one that says which side of the ledger moved and by how much costs
// a minute.
func TestEnergyConservationNamesTheOffendingSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Verify = true

	world := NewWorld(1, cfg)
	Run(world, 20)
	world.Agents[0].Energy += 7
	Step(world)

	err := world.VerifyError()
	if err == nil {
		t.Fatal("Verify accepted 7 energy from nowhere")
	}

	message := err.Error()
	if !contains(message, "ENERGY conservation off by 7") {
		t.Errorf("message does not name the side and the amount: %q", message)
	}
	for _, term := range []string{"eating", "burn", "death", "birth overhead"} {
		if !contains(message, term) {
			t.Errorf("message omits the %q term, so the reader cannot tell which one is off: %q", term, message)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestDeepRunNoOverflow runs ten times the batch length with every invariant
// on. Ages, ids and the energy accumulators all grow monotonically, so this is
// where an int16 energy field or a wrapping id would finally show up — a
// 12000-tick run is too short to reach any of those edges.
func TestDeepRunNoOverflow(t *testing.T) {
	if testing.Short() {
		t.Skip("120000 verified ticks")
	}

	cfg := DefaultConfig()
	cfg.Verify = true
	cfg.MaxTick = 120000

	world := NewWorld(1, cfg)
	previousNextID := uint32(0)

	for world.Tick < cfg.MaxTick {
		Step(world)
		if err := world.VerifyError(); err != nil {
			t.Fatalf("tick %d: %v", world.Tick, err)
		}
		if world.NextID < previousNextID {
			t.Fatalf("tick %d: NextID wrapped from %d to %d", world.Tick, previousNextID, world.NextID)
		}
		previousNextID = world.NextID
	}

	if world.Population() == 0 {
		t.Error("the deep run went extinct")
	}
	// A population an order of magnitude above the ~820 carrying capacity
	// would mean a bug, not an ecology.
	if world.Population() > 5000 {
		t.Errorf("population %d is far above the ~820 carrying capacity — that is a bug, not an ecology",
			world.Population())
	}
}
