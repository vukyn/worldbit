//go:build !nogui

package gui

import (
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

const (
	// scrubTicks is how far one arrow press moves the run; holding shift
	// multiplies it, so both "step past this famine" and "go back a thousand
	// ticks" are one gesture.
	scrubTicks     = 100
	scrubFastTicks = 1000

	// maxSeedDigits is 20, the number of decimal digits in MaxUint64. A longer
	// entry cannot be a seed, so it is refused at the keystroke rather than
	// silently truncated into a different run.
	maxSeedDigits = 20
)

// inputSource is the keyboard behind an interface.
//
// The indirection exists so the command layer is testable: the whole of the
// viewer's control surface can be driven from a table of key presses with no
// window, no GL context and no ebiten game loop, which is otherwise the least
// testable part of a GUI.
type inputSource interface {
	justPressed(key ebiten.Key) bool
	pressed(key ebiten.Key) bool
}

// keyboard is the real input source.
type keyboard struct{}

func (keyboard) justPressed(key ebiten.Key) bool { return inpututil.IsKeyJustPressed(key) }
func (keyboard) pressed(key ebiten.Key) bool     { return ebiten.IsKeyPressed(key) }

type commandKind uint8

const (
	commandQuit commandKind = iota
	commandTogglePause
	commandSetSpeed
	commandRestart
	commandBeginSeedEntry
	commandCancelSeedEntry
	commandCommitSeedEntry
	commandSeedDigit
	commandSeedBackspace
	commandScrub
)

// command is one intent read from the keyboard. Value carries the speed, the
// digit or the signed scrub distance, depending on the kind.
type command struct {
	kind  commandKind
	value int
}

// digitKeys maps 0-9 onto the main row and the numpad, so the seed entry works
// on either.
var digitKeys = [10][2]ebiten.Key{
	{ebiten.KeyDigit0, ebiten.KeyNumpad0},
	{ebiten.KeyDigit1, ebiten.KeyNumpad1},
	{ebiten.KeyDigit2, ebiten.KeyNumpad2},
	{ebiten.KeyDigit3, ebiten.KeyNumpad3},
	{ebiten.KeyDigit4, ebiten.KeyNumpad4},
	{ebiten.KeyDigit5, ebiten.KeyNumpad5},
	{ebiten.KeyDigit6, ebiten.KeyNumpad6},
	{ebiten.KeyDigit7, ebiten.KeyNumpad7},
	{ebiten.KeyDigit8, ebiten.KeyNumpad8},
	{ebiten.KeyDigit9, ebiten.KeyNumpad9},
}

// speedKeys maps the speed keys 1-4 onto ticks per displayed frame.
//
// A slice rather than a map because two keys pressed in the same frame must
// resolve the same way every time; nothing else in this project tolerates
// iteration order deciding an outcome, and the viewer is not the place to
// start.
var speedKeys = []struct {
	digit         int
	ticksPerFrame int
}{
	{digit: 1, ticksPerFrame: 1},
	{digit: 2, ticksPerFrame: 10},
	{digit: 3, ticksPerFrame: 100},
	{digit: 4, ticksPerFrame: speedMax},
}

// appendCommands reads this frame's key presses into dst.
//
// Seed entry is a mode rather than a chord: while it is active the digits mean
// digits, and outside it they mean speeds. Without the mode the same keys would
// have to do both jobs, and typing a seed would keep changing the speed.
func appendCommands(dst []command, source inputSource, entryActive bool) []command {
	if entryActive {
		return appendSeedEntryCommands(dst, source)
	}

	if source.justPressed(ebiten.KeyQ) {
		dst = append(dst, command{kind: commandQuit})
	}
	if source.justPressed(ebiten.KeySpace) {
		dst = append(dst, command{kind: commandTogglePause})
	}
	for _, speed := range speedKeys {
		if pressedDigit(source, speed.digit) {
			dst = append(dst, command{kind: commandSetSpeed, value: speed.ticksPerFrame})
		}
	}
	if source.justPressed(ebiten.KeyR) {
		dst = append(dst, command{kind: commandRestart})
	}
	if source.justPressed(ebiten.KeyS) {
		dst = append(dst, command{kind: commandBeginSeedEntry})
	}

	step := scrubTicks
	if source.pressed(ebiten.KeyShiftLeft) || source.pressed(ebiten.KeyShiftRight) {
		step = scrubFastTicks
	}
	if source.justPressed(ebiten.KeyArrowLeft) {
		dst = append(dst, command{kind: commandScrub, value: -step})
	}
	if source.justPressed(ebiten.KeyArrowRight) {
		dst = append(dst, command{kind: commandScrub, value: step})
	}

	return dst
}

func appendSeedEntryCommands(dst []command, source inputSource) []command {
	if source.justPressed(ebiten.KeyEscape) {
		return append(dst, command{kind: commandCancelSeedEntry})
	}
	if source.justPressed(ebiten.KeyEnter) || source.justPressed(ebiten.KeyNumpadEnter) {
		return append(dst, command{kind: commandCommitSeedEntry})
	}
	if source.justPressed(ebiten.KeyBackspace) {
		dst = append(dst, command{kind: commandSeedBackspace})
	}
	for digit := 0; digit < len(digitKeys); digit++ {
		if pressedDigit(source, digit) {
			dst = append(dst, command{kind: commandSeedDigit, value: digit})
		}
	}
	return dst
}

func pressedDigit(source inputSource, digit int) bool {
	keys := digitKeys[digit]
	return source.justPressed(keys[0]) || source.justPressed(keys[1])
}

// parseSeed turns the entry buffer into a seed, refusing anything that is not a
// whole uint64. An empty entry is not an error, it is a cancelled one.
func parseSeed(digits []byte) (uint64, bool) {
	if len(digits) == 0 {
		return 0, false
	}
	seed, err := strconv.ParseUint(string(digits), 10, 64)
	if err != nil {
		return 0, false
	}
	return seed, true
}
