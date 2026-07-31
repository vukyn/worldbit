//go:build nogui

package main

import (
	"errors"

	"github.com/vukyn/worldbit/internal/sim"
)

// runGUI is the headless-build stub. See gui_enabled.go for the rationale
// behind the nogui tag.
func runGUI(cfg sim.Config, seed uint64, scale int) error {
	_, _, _ = cfg, seed, scale
	return errors.New("this binary was built without GUI support (-tags nogui); run with --headless")
}
