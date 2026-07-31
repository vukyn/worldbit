//go:build nogui

package main

import "errors"

// runGUI is the headless-build stub. See gui_enabled.go for the rationale
// behind the nogui tag.
func runGUI(request guiRequest) error {
	_ = request
	return errors.New("this binary was built without GUI support (-tags nogui); run with --headless")
}
