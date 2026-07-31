// Package config resolves the simulation configuration from outside the
// binary. It is deliberately separate from internal/sim: sim never reads a
// file, never touches os, and receives a finished sim.Config value.
//
// Resolution order is default -> --config file -> --set overrides. The
// config_hash written into every output record is computed on the FINAL
// RESOLVED config, never on the file as written.
//
// No simulation parameter may ever come from an environment variable. Ambient,
// machine-local configuration is precisely the failure the determinism
// contract exists to prevent: same seed on two machines, different result, no
// visible cause. There is no legitimate use of os.Getenv in this repository.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/vukyn/worldbit/internal/sim"
)

// Resolve produces the final configuration: defaults, then the optional JSON
// file, then the --set overrides. It does NOT validate — the caller decides
// when to validate, and must do so before anything runs.
func Resolve(path string, overrides []string) (sim.Config, error) {
	cfg := sim.DefaultConfig()

	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return cfg, fmt.Errorf("read config %s: %w", path, err)
		}
		defer file.Close()

		// Decoding into an already-populated struct is what makes partial
		// config files work: absent fields simply keep their default, with no
		// pointer fields and no separate DTO.
		//
		// DisallowUnknownFields makes a mistyped key a hard error rather than a
		// silent no-op. Without it, `{"FoodMaxx": 99}` produces the default
		// config, a perfectly valid-looking config_hash, and a thousand seeds of
		// results for parameters nobody intended — the exact silent-divergence
		// failure the determinism contract exists to prevent. It also keeps the
		// file path consistent with --set, which is already strict about
		// unknown keys.
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w%s", path, err, unknownFieldHint(err))
		}
	}

	if err := ApplyOverrides(&cfg, overrides); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// unknownFieldHint appends the actionable half of an unknown-key error. The
// decoder already names the offending field; what it cannot say is that config
// keys are the PascalCase Config field names — the same names --set takes —
// and that --dump-config prints the authoritative list.
func unknownFieldHint(err error) string {
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		return ""
	}
	return " (config keys are the PascalCase parameter names, identical to --set keys; " +
		"run --dump-config to print the full list)"
}

// ApplyOverrides applies repeated --set Key=Value pairs. Keys are the Config
// field names, exactly as they appear in a dumped config file.
func ApplyOverrides(cfg *sim.Config, overrides []string) error {
	for _, override := range overrides {
		key, rawValue, found := strings.Cut(override, "=")
		if !found {
			return fmt.Errorf("--set %q: expected Key=Value", override)
		}
		key = strings.TrimSpace(key)
		rawValue = strings.TrimSpace(rawValue)
		if key == "" {
			return fmt.Errorf("--set %q: empty key", override)
		}

		if err := setField(cfg, key, rawValue); err != nil {
			return err
		}
	}
	return nil
}

func setField(cfg *sim.Config, key, rawValue string) error {
	structValue := reflect.ValueOf(cfg).Elem()
	structType := structValue.Type()

	fieldIndex := -1
	for i := 0; i < structType.NumField(); i++ {
		if strings.EqualFold(structType.Field(i).Name, key) {
			fieldIndex = i
			break
		}
	}
	if fieldIndex < 0 {
		return fmt.Errorf("--set %s: unknown parameter (see --dump-config for the full list)", key)
	}

	field := structValue.Field(fieldIndex)
	name := structType.Field(fieldIndex).Name

	switch field.Kind() {
	case reflect.Bool:
		parsed, err := strconv.ParseBool(rawValue)
		if err != nil {
			return fmt.Errorf("--set %s=%s: expected true or false", name, rawValue)
		}
		field.SetBool(parsed)
	case reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		parsed, err := strconv.ParseInt(rawValue, 10, field.Type().Bits())
		if err != nil {
			return fmt.Errorf("--set %s=%s: expected an integer that fits in %s", name, rawValue, field.Type())
		}
		field.SetInt(parsed)
	default:
		return fmt.Errorf("--set %s: unsupported field kind %s", name, field.Kind())
	}

	return nil
}

// Marshal renders a config as the canonical JSON document, with a trailing
// newline so the file is well-formed for line-oriented tools.
func Marshal(cfg sim.Config) ([]byte, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return append(data, '\n'), nil
}

// Dump writes the resolved config to a file. This is the canonical record of
// an experiment, and the way to generate a starting file to edit.
func Dump(cfg sim.Config, path string) error {
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}
