package sim

import (
	"errors"
	"reflect"
	"testing"
)

func TestDefaultConfigIsValid(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig() is invalid: %v", err)
	}
}

// TestClassificationKnobsAcceptTheirOptOut pins the opt-out that every
// classification parameter promises. The disabling value is not merely
// tolerated, it is the documented way to reproduce the behaviour from before
// each feature existed, so a stray move into the positive-values list would
// silently break that contract.
//
// The opt-out is zero for the two floors and ONE for MinOscillatingWindows,
// because that knob counts windows and its disabling value is "one window is
// enough". Zero there would not weaken the rule, it would invert it.
func TestClassificationKnobsAcceptTheirOptOut(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BurnInWindows = 0
	cfg.MinOscillatingPopulation = 0
	cfg.MinOscillatingWindows = 1

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected the opt-out values: %v", err)
	}
}

// TestValidateRejects covers every rule whose violation would otherwise fail
// silently and plausibly. MatureAge > MaxAge is the sharpest example: it makes
// every run classify EXTINCT and reads exactly like an ecology bug.
func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		mutate func(c *Config)
	}{
		{"width not a power of two", "Width", func(c *Config) { c.Width = 100 }},
		{"height not a power of two", "Height", func(c *Config) { c.Height = 96 }},
		{"width above 256", "Width", func(c *Config) { c.Width = 512 }},
		{"height above 256", "Height", func(c *Config) { c.Height = 1024 }},
		{"zero width", "Width", func(c *Config) { c.Width = 0 }},
		{"negative max tick", "MaxTick", func(c *Config) { c.MaxTick = -1 }},
		{"zero ticks per year", "TicksPerYear", func(c *Config) { c.TicksPerYear = 0 }},
		{"zero regrow ticks", "FoodRegrowTicks", func(c *Config) { c.FoodRegrowTicks = 0 }},
		{"zero burn per tick", "BurnPerTick", func(c *Config) { c.BurnPerTick = 0 }},
		{"zero search radius", "SearchRadius", func(c *Config) { c.SearchRadius = 0 }},
		{"zero init agents", "InitAgents", func(c *Config) { c.InitAgents = 0 }},
		{"init food above food max", "InitFoodPerCell", func(c *Config) { c.InitFoodPerCell = 6 }},
		{"child energy above energy max", "ChildEnergy", func(c *Config) { c.ChildEnergy = 200 }},
		{"repro cost above repro min", "ReproEnergyCost", func(c *Config) { c.ReproEnergyCost = 61 }},
		{"mature age above max age", "MatureAge", func(c *Config) { c.MatureAge = 4000 }},
		{"hunger threshold above energy max", "HungerThreshold", func(c *Config) { c.HungerThreshold = 101 }},
		{"init agent energy above energy max", "InitAgentEnergy", func(c *Config) { c.InitAgentEnergy = 101 }},
		{"regrow ticks above max tick", "FoodRegrowTicks", func(c *Config) { c.MaxTick = 100 }},
		{"more agents than cells", "InitAgents", func(c *Config) { c.InitAgents = 20000 }},
		{"search radius above grid", "SearchRadius", func(c *Config) { c.SearchRadius = 200 }},
		// The classification knobs are checked outside the positive list,
		// because their legal ranges differ from it. Zero is legal and
		// meaningful for the two floors, so only negatives are rejected there.
		{"negative burn-in windows", "BurnInWindows", func(c *Config) { c.BurnInWindows = -1 }},
		{"negative oscillation floor", "MinOscillatingPopulation", func(c *Config) {
			c.MinOscillatingPopulation = -1
		}},
		// MinOscillatingWindows is the exception: its opt-out is 1, so zero is
		// rejected as well. Zero would satisfy "at least zero windows fired"
		// for every run and make OSCILLATING the universal fallback — a config
		// that runs happily and mislabels the whole sweep.
		{"zero oscillating windows", "MinOscillatingWindows", func(c *Config) {
			c.MinOscillatingWindows = 0
		}},
		{"negative oscillating windows", "MinOscillatingWindows", func(c *Config) {
			c.MinOscillatingWindows = -1
		}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := DefaultConfig()
			testCase.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() accepted an invalid config (%s)", testCase.name)
			}

			var configError *ConfigError
			if !errors.As(err, &configError) {
				t.Fatalf("Validate() returned %T, want *ConfigError", err)
			}
			if configError.Field != testCase.field {
				t.Errorf("Validate() blamed %q, want %q (message: %v)", configError.Field, testCase.field, err)
			}
		})
	}
}

// TestConfigHashChangesWithEveryField reflectively perturbs each field in turn
// and asserts the hash moves.
//
// This is the guard against the quietest possible failure: a field added to
// Config but forgotten in Hash(). Two materially different configs would then
// share a config_hash, and results from incompatible parameter sets would
// silently be treated as comparable.
func TestConfigHashChangesWithEveryField(t *testing.T) {
	baseline := DefaultConfig()
	baselineHash := baseline.Hash()

	configType := reflect.TypeOf(baseline)
	seen := make(map[uint64]string, configType.NumField()+1)
	seen[baselineHash] = "<default>"

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)

		if field.Tag.Get("json") == "" {
			t.Errorf("field %s has no json tag — it would not survive --config or --dump-config", field.Name)
		}

		mutated := DefaultConfig()
		value := reflect.ValueOf(&mutated).Elem().Field(i)
		switch value.Kind() {
		case reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			value.SetInt(value.Int() + 1)
		case reflect.Bool:
			value.SetBool(!value.Bool())
		default:
			t.Fatalf("field %s has unhandled kind %s — extend this test", field.Name, value.Kind())
		}

		mutatedHash := mutated.Hash()
		if mutatedHash == baselineHash {
			t.Errorf("changing %s did not change Config.Hash() — add it to Hash()", field.Name)
			continue
		}
		if other, collides := seen[mutatedHash]; collides {
			t.Errorf("changing %s produces the same hash as changing %s", field.Name, other)
			continue
		}
		seen[mutatedHash] = field.Name
	}
}
