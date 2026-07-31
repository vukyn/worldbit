package sim

import "math/bits"

// Config is the single source of truth for every simulation parameter.
// Nothing in the simulation may hardcode a value that belongs here.
//
// The struct is simultaneously the in-memory config and the on-disk JSON
// schema — one definition, no DTO to keep in sync. JSON keys are the Go field
// names, which is also what --set accepts (--set InitFoodPerCell=3).
//
// Adding a field means adding it to Hash(), to Validate() where a rule
// applies, and giving it a json tag. TestConfigHashChangesWithEveryField
// enforces the first: a field missing from Hash() would make two different
// configs look identical, which quietly voids the determinism contract.
type Config struct {
	Width  int32 `json:"Width"`  // MUST be a power of two — torus masking
	Height int32 `json:"Height"` // MUST be a power of two — torus masking

	TicksPerYear int32 `json:"TicksPerYear"`
	MaxTick      int32 `json:"MaxTick"`

	FoodRegrowTicks int32 `json:"FoodRegrowTicks"` // each cell regrows once per this many ticks
	FoodMax         int16 `json:"FoodMax"`
	InitFoodPerCell int16 `json:"InitFoodPerCell"`

	EnergyMax       int16 `json:"EnergyMax"`
	EnergyPerFood   int16 `json:"EnergyPerFood"`
	BurnPerTick     int16 `json:"BurnPerTick"`
	HungerThreshold int16 `json:"HungerThreshold"`

	MatureAge     int32 `json:"MatureAge"`
	BirthCooldown int32 `json:"BirthCooldown"`
	MaxAge        int32 `json:"MaxAge"`

	ReproEnergyMin  int16 `json:"ReproEnergyMin"`
	ReproEnergyCost int16 `json:"ReproEnergyCost"` // parent pays
	ChildEnergy     int16 `json:"ChildEnergy"`     // remainder is birth overhead

	InitAgents      int32 `json:"InitAgents"`
	InitAgentEnergy int16 `json:"InitAgentEnergy"`
	InitAgeSpread   int32 `json:"InitAgeSpread"` // initial ages uniform in [0, InitAgeSpread)
	SearchRadius    int32 `json:"SearchRadius"`  // Chebyshev cap on nearest-food search

	// BurnInWindows is a CLASSIFICATION parameter, not a simulation one: the
	// simulation never reads it, and changing it cannot move a single state
	// hash. It lives here for two reasons. It must travel in config_hash,
	// because two runs classified with different burn-ins are not comparable
	// even when their trajectories are bit-identical; and a parameter sweep
	// addresses its axes by Config field name, so a knob that is not a Config
	// field cannot be swept. MaxTick is here on the same footing.
	//
	// See stats.ClassifierConfig.BurnInWindows for what it does. Zero opts out.
	BurnInWindows int32 `json:"BurnInWindows"`

	// MinOscillatingPopulation is a CLASSIFICATION parameter on the same
	// footing as BurnInWindows: the simulation never reads it, it cannot move
	// a state hash, and it lives here so that it travels in config_hash and can
	// be swept by name.
	//
	// It is the mean population a window must reach before its coefficient of
	// variation may count as high variation. Without it, the scale-free
	// coefficient of variation labels any small starving remnant OSCILLATING.
	//
	// See stats.ClassifierConfig.MinOscillatingPopulation for the derivation of
	// the default. Zero opts out.
	MinOscillatingPopulation int32 `json:"MinOscillatingPopulation"`

	Verify bool `json:"Verify"`
}

// DefaultConfig returns the ratified parameter set. Carrying-capacity
// arithmetic for these values: 16384 cells / 200 ticks * 10 energy per food /
// 1 energy burnt per tick is roughly 820 sustainable agents, so a population
// above ~5000 means a bug, not an ecology.
func DefaultConfig() Config {
	return Config{
		Width:  128,
		Height: 128,

		TicksPerYear: 120,
		MaxTick:      12000,

		FoodRegrowTicks: 200,
		FoodMax:         5,
		InitFoodPerCell: 1,

		EnergyMax:       100,
		EnergyPerFood:   10,
		BurnPerTick:     1,
		HungerThreshold: 70,

		MatureAge:     240,
		BirthCooldown: 60,
		MaxAge:        3000,

		ReproEnergyMin:  60,
		ReproEnergyCost: 40,
		ChildEnergy:     30,

		InitAgents:      50,
		InitAgentEnergy: 50,
		InitAgeSpread:   240,
		SearchRadius:    12,

		BurnInWindows:            1,
		MinOscillatingPopulation: 64,

		Verify: false,
	}
}

// ConfigError names the offending field so an invalid external config fails
// loudly and specifically, before a single tick runs.
type ConfigError struct {
	Field  string
	Reason string
}

func (e *ConfigError) Error() string {
	return "config: " + e.Field + ": " + e.Reason
}

// Validate rejects configurations whose failure mode would otherwise be silent
// and plausible-looking rather than a crash. External config means invalid
// values will arrive; the dangerous ones read exactly like ecology bugs.
func (c Config) Validate() error {
	positive := []struct {
		field string
		value int32
	}{
		{"Width", c.Width},
		{"Height", c.Height},
		{"TicksPerYear", c.TicksPerYear},
		{"MaxTick", c.MaxTick},
		{"FoodRegrowTicks", c.FoodRegrowTicks},
		{"FoodMax", int32(c.FoodMax)},
		{"InitFoodPerCell", int32(c.InitFoodPerCell)},
		{"EnergyMax", int32(c.EnergyMax)},
		{"EnergyPerFood", int32(c.EnergyPerFood)},
		{"BurnPerTick", int32(c.BurnPerTick)},
		{"HungerThreshold", int32(c.HungerThreshold)},
		{"MatureAge", c.MatureAge},
		{"BirthCooldown", c.BirthCooldown},
		{"MaxAge", c.MaxAge},
		{"ReproEnergyMin", int32(c.ReproEnergyMin)},
		{"ReproEnergyCost", int32(c.ReproEnergyCost)},
		{"ChildEnergy", int32(c.ChildEnergy)},
		{"InitAgents", c.InitAgents},
		{"InitAgentEnergy", int32(c.InitAgentEnergy)},
		{"InitAgeSpread", c.InitAgeSpread},
		{"SearchRadius", c.SearchRadius},
	}
	for _, item := range positive {
		if item.value <= 0 {
			return &ConfigError{Field: item.field, Reason: "must be positive"}
		}
	}

	// Zero is legal and meaningful for both of these — it opts the classifier
	// out of burn-in and out of the high-variation floor respectively — so they
	// are checked separately from the positive list.
	if c.BurnInWindows < 0 {
		return &ConfigError{Field: "BurnInWindows", Reason: "must not be negative (0 disables burn-in)"}
	}
	if c.MinOscillatingPopulation < 0 {
		return &ConfigError{
			Field:  "MinOscillatingPopulation",
			Reason: "must not be negative (0 disables the high-variation population floor)",
		}
	}

	// Torus masking (idx = (y<<shift)|x, wrap = &mask) requires powers of two,
	// and agent coordinates are uint8, so 256 is the hard ceiling.
	dimensions := []struct {
		field string
		value int32
	}{{"Width", c.Width}, {"Height", c.Height}}
	for _, item := range dimensions {
		if item.value > 256 {
			return &ConfigError{Field: item.field, Reason: "must be <= 256 (agent coordinates are uint8)"}
		}
		if bits.OnesCount32(uint32(item.value)) != 1 {
			return &ConfigError{Field: item.field, Reason: "must be a power of two (torus masking)"}
		}
	}

	if c.InitFoodPerCell > c.FoodMax {
		return &ConfigError{Field: "InitFoodPerCell", Reason: "must not exceed FoodMax; regrowth could never restore the initial condition"}
	}
	if c.ChildEnergy > c.EnergyMax {
		return &ConfigError{Field: "ChildEnergy", Reason: "must not exceed EnergyMax"}
	}
	if c.ReproEnergyCost > c.ReproEnergyMin {
		return &ConfigError{Field: "ReproEnergyCost", Reason: "must not exceed ReproEnergyMin; the parent would end a birth with negative energy"}
	}
	if c.MatureAge > c.MaxAge {
		return &ConfigError{Field: "MatureAge", Reason: "must not exceed MaxAge; nothing could ever reproduce and every run would be EXTINCT"}
	}
	if c.HungerThreshold > c.EnergyMax {
		return &ConfigError{Field: "HungerThreshold", Reason: "must not exceed EnergyMax"}
	}
	if c.InitAgentEnergy > c.EnergyMax {
		return &ConfigError{Field: "InitAgentEnergy", Reason: "must not exceed EnergyMax"}
	}
	if c.FoodRegrowTicks > c.MaxTick {
		return &ConfigError{Field: "FoodRegrowTicks", Reason: "must not exceed MaxTick; no cell would regrow twice in a run"}
	}
	if c.InitAgents > c.Width*c.Height {
		return &ConfigError{Field: "InitAgents", Reason: "must not exceed the number of cells"}
	}
	if c.SearchRadius > c.Width || c.SearchRadius > c.Height {
		return &ConfigError{Field: "SearchRadius", Reason: "must not exceed the grid dimensions"}
	}
	// The Chebyshev ring offset table is precomputed once at process start, so
	// it has a fixed bound. The bound is not arbitrary: past half the grid a
	// torus ring wraps onto itself and revisits cells, so a larger radius would
	// not look further, it would look at the same cells twice.
	if c.SearchRadius > maxRingRadius {
		return &ConfigError{Field: "SearchRadius", Reason: "must not exceed 64 (the precomputed ring table); a larger radius wraps around the torus and rescans the same cells"}
	}

	return nil
}

// Hash is an FNV-1a 64 digest over a fixed-order little-endian encoding of
// every field. It is computed on the FINAL RESOLVED config (defaults, then
// file, then flag overrides) and written into every output record: two records
// with different config_hash are not comparable.
func (c Config) Hash() uint64 {
	hash := uint64(fnvOffset64)

	hash = mixInt32(hash, c.Width)
	hash = mixInt32(hash, c.Height)
	hash = mixInt32(hash, c.TicksPerYear)
	hash = mixInt32(hash, c.MaxTick)
	hash = mixInt32(hash, c.FoodRegrowTicks)
	hash = mixInt16(hash, c.FoodMax)
	hash = mixInt16(hash, c.InitFoodPerCell)
	hash = mixInt16(hash, c.EnergyMax)
	hash = mixInt16(hash, c.EnergyPerFood)
	hash = mixInt16(hash, c.BurnPerTick)
	hash = mixInt16(hash, c.HungerThreshold)
	hash = mixInt32(hash, c.MatureAge)
	hash = mixInt32(hash, c.BirthCooldown)
	hash = mixInt32(hash, c.MaxAge)
	hash = mixInt16(hash, c.ReproEnergyMin)
	hash = mixInt16(hash, c.ReproEnergyCost)
	hash = mixInt16(hash, c.ChildEnergy)
	hash = mixInt32(hash, c.InitAgents)
	hash = mixInt16(hash, c.InitAgentEnergy)
	hash = mixInt32(hash, c.InitAgeSpread)
	hash = mixInt32(hash, c.SearchRadius)
	hash = mixInt32(hash, c.BurnInWindows)
	hash = mixInt32(hash, c.MinOscillatingPopulation)
	hash = mixBool(hash, c.Verify)

	return hash
}
