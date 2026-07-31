package sim

import (
	"encoding/binary"
	"hash/fnv"
	"testing"
)

// TestMixMatchesStandardFNV checks the hand-inlined mixers against stdlib
// hash/fnv over the same byte sequence.
//
// The inlined version exists to avoid the hash.Hash64 interface and its
// allocation on a call made at every checkpoint. That optimisation is only
// safe if it is bit-identical to real FNV-1a: a subtly wrong constant would
// still be perfectly deterministic, so golden fixtures would happily pin the
// wrong algorithm forever and nothing else would ever notice.
func TestMixMatchesStandardFNV(t *testing.T) {
	reference := func(data []byte) uint64 {
		digest := fnv.New64a()
		digest.Write(data)
		return digest.Sum64()
	}

	if got := uint64(fnvOffset64); got != reference(nil) {
		t.Fatalf("fnvOffset64 = %d, stdlib empty digest = %d", got, reference(nil))
	}

	for _, value := range []uint8{0, 1, 42, 255} {
		if got := mixByte(fnvOffset64, value); got != reference([]byte{value}) {
			t.Errorf("mixByte(%d) = %016x, want %016x", value, got, reference([]byte{value}))
		}
	}

	for _, value := range []uint16{0, 1, 0x1234, 0xFFFF} {
		buffer := make([]byte, 2)
		binary.LittleEndian.PutUint16(buffer, value)
		if got := mixUint16(fnvOffset64, value); got != reference(buffer) {
			t.Errorf("mixUint16(%d) = %016x, want %016x", value, got, reference(buffer))
		}
	}

	for _, value := range []uint32{0, 1, 0xDEADBEEF, 0xFFFFFFFF} {
		buffer := make([]byte, 4)
		binary.LittleEndian.PutUint32(buffer, value)
		if got := mixUint32(fnvOffset64, value); got != reference(buffer) {
			t.Errorf("mixUint32(%d) = %016x, want %016x", value, got, reference(buffer))
		}
	}

	// Signed values must go through the same two's-complement little-endian
	// encoding, including the negative sentinels the simulation relies on
	// (TargetIdx = -1, LastBirthTick = -BirthCooldown).
	for _, value := range []int32{0, 1, -1, -60, 2147483647, -2147483648} {
		buffer := make([]byte, 4)
		binary.LittleEndian.PutUint32(buffer, uint32(value))
		if got := mixInt32(fnvOffset64, value); got != reference(buffer) {
			t.Errorf("mixInt32(%d) = %016x, want %016x", value, got, reference(buffer))
		}
	}
	for _, value := range []int16{0, 1, -1, 32767, -32768} {
		buffer := make([]byte, 2)
		binary.LittleEndian.PutUint16(buffer, uint16(value))
		if got := mixInt16(fnvOffset64, value); got != reference(buffer) {
			t.Errorf("mixInt16(%d) = %016x, want %016x", value, got, reference(buffer))
		}
	}

	if got := mixBool(fnvOffset64, true); got != reference([]byte{1}) {
		t.Errorf("mixBool(true) = %016x, want %016x", got, reference([]byte{1}))
	}
	if got := mixBool(fnvOffset64, false); got != reference([]byte{0}) {
		t.Errorf("mixBool(false) = %016x, want %016x", got, reference([]byte{0}))
	}

	// Chaining must equal digesting the concatenation.
	chainedValue := int32(-3)
	chained := mixInt32(mixByte(fnvOffset64, 7), chainedValue)
	buffer := []byte{7, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(buffer[1:], uint32(chainedValue))
	if chained != reference(buffer) {
		t.Errorf("chained mix = %016x, want %016x", chained, reference(buffer))
	}
}

// TestHashDoesNotAllocate protects the checkpoint cost: Hash walks a ~66 KB
// working set and is called often enough that an accidental allocation (for
// example by reintroducing the hash.Hash64 interface) would show up as run
// time rather than as a test failure.
func TestHashDoesNotAllocate(t *testing.T) {
	world := NewWorld(1, DefaultConfig())
	allocations := testing.AllocsPerRun(10, func() { _ = Hash(world) })
	if allocations != 0 {
		t.Errorf("Hash allocated %v times per call, want 0", allocations)
	}
}

func BenchmarkHash(b *testing.B) {
	world := NewWorld(1, DefaultConfig())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Hash(world)
	}
}

func BenchmarkStep(b *testing.B) {
	world := NewWorld(1, DefaultConfig())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Step(world)
	}
}

func BenchmarkRun12000(b *testing.B) {
	cfg := DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		world := NewWorld(1, cfg)
		Run(world, 12000)
	}
}
