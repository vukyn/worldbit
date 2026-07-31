//go:build !nogui

package main

import (
	"github.com/vukyn/worldbit/internal/gui"
)

// runGUI opens the viewer. The viewer replays a seed by RE-SIMULATING it rather
// than by loading snapshots, which is only sound because the simulation is
// bit-exact deterministic.
//
// This file is the seam. Its counterpart gui_stub.go carries the //go:build
// nogui variant, so `go build -tags nogui` never links a graphics stack — the
// batch harness is the primary deliverable and must stay buildable on a CI
// runner or server with no GL/X11 headers. Both tag combinations are verified
// on every change so the boundary cannot rot.
//
// gui.Run calls ebiten.RunGame, which must own the main goroutine, so this is
// called directly from the CLI action and never from a goroutine of its own.
func runGUI(request guiRequest) error {
	return gui.Run(gui.Options{
		Config:     request.Config,
		Seed:       request.Seed,
		Scale:      request.Scale,
		Classifier: request.Classifier,
		ExpectHash: request.ExpectHash,
		HasExpect:  request.HasExpect,
		Version:    request.Version,
	})
}
