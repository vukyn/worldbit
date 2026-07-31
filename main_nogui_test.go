//go:build nogui

package main

import (
	"strings"
	"testing"
)

// TestGUIDispatchWithoutGUI pins the mode dispatch: no --headless means the
// viewer, and on a binary built without one that has to fail with an
// explanation rather than either succeeding or crashing.
//
// It lives on the nogui build because that is the only build where exercising
// this path is safe to do in a test — the other one would open a window.
func TestGUIDispatchWithoutGUI(t *testing.T) {
	err := newApp().Run([]string{"worldbit", "--seed", "3"})
	if err == nil {
		t.Fatal("a binary built with -tags nogui reported a successful GUI run")
	}
	if !strings.Contains(err.Error(), "--headless") {
		t.Errorf("error %q does not tell the user how to run headless", err)
	}
}

// TestHeadlessStillRunsWithoutGUI is the whole point of the build tag: the
// batch harness, which is the primary deliverable, must work on a machine that
// cannot build a graphics stack at all.
func TestHeadlessStillRunsWithoutGUI(t *testing.T) {
	out := t.TempDir() + "/runs.csv"
	err := newApp().Run([]string{
		"worldbit", "--headless", "--seed", "1", "--runs", "1", "--ticks", "200", "--out", out,
	})
	if err != nil {
		t.Fatalf("headless run on a nogui build: %v", err)
	}
}
