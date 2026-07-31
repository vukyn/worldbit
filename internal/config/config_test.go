package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vukyn/worldbit/internal/sim"
)

// TestConfigRoundTrip is the guarantee behind --dump-config as an experiment
// record: dumping a config and loading it back must reproduce it exactly,
// hash included.
func TestConfigRoundTrip(t *testing.T) {
	original, err := Resolve("", []string{"InitFoodPerCell=3", "FoodMax=8", "Verify=true", "MaxTick=24000"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := Dump(original, path); err != nil {
		t.Fatalf("dump: %v", err)
	}

	reloaded, err := Resolve(path, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	if reloaded != original {
		t.Errorf("round trip changed the config:\n got %+v\nwant %+v", reloaded, original)
	}
	if reloaded.Hash() != original.Hash() {
		t.Errorf("round trip changed the hash: %016x != %016x", reloaded.Hash(), original.Hash())
	}
}

// TestPartialConfigKeepsDefaults is why the loader decodes into an
// already-populated struct: a two-line config file must not zero out every
// parameter it does not mention.
//
// This is the permissive half of the file contract, and it is deliberately
// exhaustive: it asserts that EVERY field the file does not mention is still
// exactly its default. Rejecting unknown keys is one small step away from
// accidentally requiring every key, and that regression would only show up as
// "my two-line config file stopped working".
func TestPartialConfigKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.json")
	if err := os.WriteFile(path, []byte(`{"InitFoodPerCell": 4, "MaxTick": 500}`), 0o644); err != nil {
		t.Fatalf("write partial config: %v", err)
	}

	cfg, err := Resolve(path, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if cfg.InitFoodPerCell != 4 {
		t.Errorf("InitFoodPerCell = %d, want 4", cfg.InitFoodPerCell)
	}
	if cfg.MaxTick != 500 {
		t.Errorf("MaxTick = %d, want 500", cfg.MaxTick)
	}

	// Every other field must be untouched. Expected is the default config with
	// only the two mentioned fields applied, so any drift anywhere else fails.
	expected := sim.DefaultConfig()
	expected.InitFoodPerCell = 4
	expected.MaxTick = 500
	if cfg != expected {
		t.Errorf("a partial config disturbed unmentioned parameters:\n got %+v\nwant %+v", cfg, expected)
	}

	defaults := sim.DefaultConfig()
	if cfg.Hash() == defaults.Hash() {
		t.Error("a partial config that changes two fields produced the default hash")
	}
}

// TestConfigRejectsUnknownField is the strict half of the file contract.
//
// A mistyped key that is silently ignored is the worst possible outcome here:
// the run produces a valid-looking config_hash for parameters nobody chose,
// and nothing in the recorded output reveals it. That is precisely the silent
// divergence the determinism design exists to prevent, so an unknown key is a
// hard error, exactly as it already is for --set.
func TestConfigRejectsUnknownField(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		field string
	}{
		{"typo in a real name", `{"FoodMaxx": 99}`, "FoodMaxx"},
		{"snake_case instead of PascalCase", `{"food_max": 9}`, "food_max"},
		{"bogus key alongside valid ones", `{"MaxTick": 500, "totally_bogus": 1}`, "totally_bogus"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "typo.json")
			if err := os.WriteFile(path, []byte(testCase.body), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			_, err := Resolve(path, nil)
			if err == nil {
				t.Fatal("Resolve accepted a config file with an unknown key")
			}
			if !strings.Contains(err.Error(), testCase.field) {
				t.Errorf("error %q does not name the offending field %q", err.Error(), testCase.field)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name the config file", err.Error())
			}
			if !strings.Contains(err.Error(), "--dump-config") {
				t.Errorf("error %q does not point at --dump-config for the valid key list", err.Error())
			}
		})
	}
}

// TestConfigRejectsMalformedJSON keeps a broken file loud too.
func TestConfigRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte(`{"MaxTick": }`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Resolve(path, nil); err == nil {
		t.Fatal("Resolve accepted malformed JSON")
	}
}

// TestResolutionOrderFlagsWinOverFile pins default -> file -> --set.
func TestResolutionOrderFlagsWinOverFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"FoodMax": 9, "MaxTick": 500}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Resolve(path, []string{"FoodMax=2"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if cfg.FoodMax != 2 {
		t.Errorf("FoodMax = %d, want 2 (--set must win over the file)", cfg.FoodMax)
	}
	if cfg.MaxTick != 500 {
		t.Errorf("MaxTick = %d, want 500 (the file must win over the default)", cfg.MaxTick)
	}
	if cfg.EnergyMax != sim.DefaultConfig().EnergyMax {
		t.Error("an unmentioned parameter drifted from its default")
	}
}

func TestOverrideErrors(t *testing.T) {
	cases := []struct {
		name     string
		override string
		contains string
	}{
		{"missing equals", "FoodMax", "expected Key=Value"},
		{"empty key", "=5", "empty key"},
		{"unknown key", "Nonsense=5", "unknown parameter"},
		{"not an integer", "FoodMax=lots", "expected an integer"},
		{"out of range for int16", "FoodMax=40000", "expected an integer"},
		{"not a bool", "Verify=maybe", "expected true or false"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := sim.DefaultConfig()
			err := ApplyOverrides(&cfg, []string{testCase.override})
			if err == nil {
				t.Fatalf("ApplyOverrides accepted %q", testCase.override)
			}
			if !strings.Contains(err.Error(), testCase.contains) {
				t.Errorf("error %q does not mention %q", err.Error(), testCase.contains)
			}
		})
	}
}

func TestResolveMissingFileIsAnError(t *testing.T) {
	if _, err := Resolve(filepath.Join(t.TempDir(), "absent.json"), nil); err == nil {
		t.Fatal("Resolve accepted a nonexistent config path")
	}
}

// TestDumpedConfigMentionsEveryField makes --dump-config trustworthy as the
// full, self-generating parameter reference.
func TestDumpedConfigMentionsEveryField(t *testing.T) {
	data, err := Marshal(sim.DefaultConfig())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(data)

	for _, field := range []string{
		"Width", "Height", "TicksPerYear", "MaxTick", "FoodRegrowTicks", "FoodMax",
		"InitFoodPerCell", "EnergyMax", "EnergyPerFood", "BurnPerTick", "HungerThreshold",
		"MatureAge", "BirthCooldown", "MaxAge", "ReproEnergyMin", "ReproEnergyCost",
		"ChildEnergy", "InitAgents", "InitAgentEnergy", "InitAgeSpread", "SearchRadius", "Verify",
	} {
		if !strings.Contains(text, `"`+field+`"`) {
			t.Errorf("dumped config does not contain %q", field)
		}
	}
}
