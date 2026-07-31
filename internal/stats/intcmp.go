// Package stats classifies the outcome of a simulation run from nothing but
// the per-tick population count.
//
// It deliberately knows nothing about internal/sim: population arrives as a
// plain integer, one sample per tick. That keeps the classifier independently
// testable against hand-built series, and keeps every statistics concern out of
// the simulation.
//
// All arithmetic here is exact integer arithmetic. Not tidiness: Go may fuse
// a*b+c into a single FMA instruction, and whether it does differs between
// arm64 and amd64, so a floating-point classifier could file the same run under
// two different outcomes on two machines despite bit-identical simulation
// state. The variance and slope predicates are therefore expressed as integer
// comparisons, and the two that overflow int64 are compared as exact 128-bit
// products by mulCmpU128 below.
package stats

import "math/bits"

// mulCmpU128 compares the exact products a·b and c·d, returning -1, 0 or +1 as
// a·b is less than, equal to, or greater than c·d.
//
// Both products are computed in full 128-bit precision via bits.Mul64, so the
// comparison is exact even when either product exceeds 2^64 — which the slope
// predicates do by four orders of magnitude (≈1.6e20 at the default window
// size). No math/big, no allocation, no floats: this is a handful of
// instructions and it is the reason the classifier can be exact without
// pulling arbitrary-precision arithmetic into the hot path.
func mulCmpU128(a, b, c, d uint64) int {
	leftHigh, leftLow := bits.Mul64(a, b)
	rightHigh, rightLow := bits.Mul64(c, d)

	if leftHigh != rightHigh {
		if leftHigh < rightHigh {
			return -1
		}
		return 1
	}
	if leftLow != rightLow {
		if leftLow < rightLow {
			return -1
		}
		return 1
	}
	return 0
}
