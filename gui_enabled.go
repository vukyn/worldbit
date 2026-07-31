//go:build !nogui

package main

import (
	"errors"

	"github.com/vukyn/worldbit/internal/sim"
)

// runGUI opens the viewer. The viewer replays a seed by RE-SIMULATING it
// rather than by loading snapshots, which is only sound because the
// simulation is bit-exact deterministic.
//
// internal/gui and its ebiten dependency arrive in a later phase; this file is
// the seam. Its counterpart gui_stub.go carries the //go:build nogui variant,
// so `go build -tags nogui` never links a graphics stack — the batch harness
// is the primary deliverable and must stay buildable on a CI runner or server
// with no GL/X11 headers. Both tag combinations are verified from day one so
// the boundary cannot rot.
func runGUI(cfg sim.Config, seed uint64, scale int) error {
	_, _, _ = cfg, seed, scale
	return errors.New("the GUI is not built yet; run with --headless")
}
