package stats

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

// bigMulCmp is the arbitrary-precision reference for mulCmpU128.
//
// It lives in the test file and nowhere else, on purpose: math/big allocates
// and is far slower than two MUL instructions, and the whole point of
// mulCmpU128 is to get exactness without paying for it in the classifier. The
// reference exists to prove the cheap version right, not to be shipped.
func bigMulCmp(a, b, c, d uint64) int {
	left := new(big.Int).Mul(new(big.Int).SetUint64(a), new(big.Int).SetUint64(b))
	right := new(big.Int).Mul(new(big.Int).SetUint64(c), new(big.Int).SetUint64(d))
	return left.Cmp(right)
}

// TestIntCmpMatchesBigInt compares mulCmpU128 against math/big over hand-picked
// edge cases and a large randomised sample.
//
// The randomisation is spread over magnitude classes rather than drawn
// uniformly from the whole uint64 range: uniform draws are almost always
// enormous and almost never equal, so they would never exercise the
// equal-high-word path where the comparison falls through to the low words —
// which is precisely the path a naive "compare the high words" implementation
// gets wrong.
func TestIntCmpMatchesBigInt(t *testing.T) {
	edgeCases := [][4]uint64{
		{0, 0, 0, 0},
		{0, math.MaxUint64, math.MaxUint64, 0},
		{1, 1, 1, 1},
		{1, 1, 1, 2},
		{1, 2, 1, 1},
		{math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64},
		{math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64 - 1},
		{math.MaxUint64, 1, 1, math.MaxUint64},
		{math.MaxUint64, 2, 2, math.MaxUint64},
		{1 << 32, 1 << 32, 1 << 63, 2},
		{1 << 63, 2, 1 << 32, 1 << 32},
		// Equal high words, differing low words: 2^64 + 1 against 2^64 + 2.
		{1 << 32, 1<<32 + 1, 1 << 32, 1<<32 + 2},
		// The magnitudes the slope predicates actually reach at the default
		// window size: 20·n²·|N| against D·S1.
		{20 * 1200 * 1200, 5_500_000_000_000, 172_799_880_000, 7_863_600},
	}

	for _, operands := range edgeCases {
		a, b, c, d := operands[0], operands[1], operands[2], operands[3]
		if got, want := mulCmpU128(a, b, c, d), bigMulCmp(a, b, c, d); got != want {
			t.Errorf("mulCmpU128(%d, %d, %d, %d) = %d, math/big says %d", a, b, c, d, got, want)
		}
	}

	// Fixed seed: a failure here must be reproducible from the test name alone.
	random := rand.New(rand.NewPCG(0x5EED, 0xC0FFEE))

	// Each class is a bit width; drawing both factors of a pair from the same
	// class keeps the two products comparable often enough that ties and
	// near-ties actually occur.
	widths := []uint{1, 8, 16, 31, 32, 33, 48, 63, 64}

	draw := func(width uint) uint64 {
		if width >= 64 {
			return random.Uint64()
		}
		return random.Uint64() & ((1 << width) - 1)
	}

	const rounds = 200_000
	for round := 0; round < rounds; round++ {
		width := widths[random.IntN(len(widths))]
		a, b := draw(width), draw(width)

		var c, d uint64
		switch round % 4 {
		case 0:
			c, d = draw(width), draw(width)
		case 1:
			// Exactly equal products.
			c, d = a, b
		case 2:
			// Off by one in a single factor: forces the low-word comparison.
			c, d = a, b+1
		case 3:
			// Same product reached by a different factorisation, when the
			// halving is exact; otherwise a near miss, which is just as useful.
			c, d = a/2, b*2
		}

		if got, want := mulCmpU128(a, b, c, d), bigMulCmp(a, b, c, d); got != want {
			t.Fatalf("round %d: mulCmpU128(%d, %d, %d, %d) = %d, math/big says %d",
				round, a, b, c, d, got, want)
		}
	}
}

// TestIntCmpIsAntisymmetric pins the sign convention, which every predicate in
// window.go depends on reading correctly.
func TestIntCmpIsAntisymmetric(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 11))

	for round := 0; round < 10_000; round++ {
		a, b := random.Uint64(), random.Uint64()
		c, d := random.Uint64(), random.Uint64()

		forward := mulCmpU128(a, b, c, d)
		backward := mulCmpU128(c, d, a, b)

		if forward != -backward {
			t.Fatalf("round %d: mulCmpU128(%d,%d,%d,%d) = %d but the reverse is %d",
				round, a, b, c, d, forward, backward)
		}
	}
}
