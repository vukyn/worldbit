package sim

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "regenerate testdata/golden_hashes.csv")

// goldenSeeds and goldenTicks pin the regression fixture.
//
// Until agents exist the trajectory is seed-independent (only regrowth runs),
// so every seed currently yields the same digest. The fixture is written with
// all eight seeds anyway so that the shape is already right when agents make
// the seeds diverge.
var goldenSeeds = []uint64{1, 2, 3, 4, 5, 6, 7, 8}

var goldenTicks = []int32{100, 1000, 12000}

const goldenPath = "../../testdata/golden_hashes.csv"

// TestSameSeedSameHash is the core determinism assertion: two worlds built
// from the same seed and config must be bit-identical at every checkpoint for
// a full 12000-tick run. On divergence it reports the FIRST bad tick, which is
// the only tick worth debugging.
func TestSameSeedSameHash(t *testing.T) {
	const totalTicks = 12000
	const checkpoint = 100

	cfg := DefaultConfig()
	left := NewWorld(1, cfg)
	right := NewWorld(1, cfg)

	for tick := int32(0); tick < totalTicks; tick += checkpoint {
		Run(left, tick+checkpoint)
		Run(right, tick+checkpoint)

		leftHash := Hash(left)
		rightHash := Hash(right)
		if leftHash != rightHash {
			t.Fatalf("determinism broken: first divergence at tick %d (%016x != %016x)",
				left.Tick, leftHash, rightHash)
		}
	}

	if left.Tick != totalTicks {
		t.Fatalf("expected tick %d, got %d", totalTicks, left.Tick)
	}
}

// TestGoldenHashes pins the trajectory against a committed fixture, so a
// behavioural change cannot slip through as "still deterministic".
//
// Regenerate with `make golden` ONLY when a behavioural change justifies it,
// and say which change in the commit message. A golden update riding along
// with a "pure refactor" is a red flag to investigate, never to accept.
func TestGoldenHashes(t *testing.T) {
	if *update {
		writeGoldenFile(t)
		return
	}

	expected := readGoldenFile(t)
	if len(expected) == 0 {
		t.Fatal("testdata/golden_hashes.csv is empty — regenerate with `make golden`")
	}

	cfg := DefaultConfig()
	index := 0
	for _, seed := range goldenSeeds {
		world := NewWorld(seed, cfg)
		for _, tick := range goldenTicks {
			Run(world, tick)
			actual := Hash(world)

			if index >= len(expected) {
				t.Fatalf("golden file has %d rows, expected at least %d — regenerate with `make golden`",
					len(expected), index+1)
			}
			row := expected[index]
			index++

			if row.seed != seed || row.tick != tick {
				t.Fatalf("golden row %d is seed=%d tick=%d, expected seed=%d tick=%d",
					index, row.seed, row.tick, seed, tick)
			}
			if row.hash != actual {
				t.Errorf("seed %d tick %d: hash %016x, golden %016x", seed, tick, actual, row.hash)
			}
		}
	}

	if index != len(expected) {
		t.Errorf("golden file has %d rows, the run produced %d", len(expected), index)
	}
}

type goldenRow struct {
	seed uint64
	tick int32
	hash uint64
}

func writeGoldenFile(t *testing.T) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
		t.Fatalf("create testdata dir: %v", err)
	}
	file, err := os.Create(goldenPath)
	if err != nil {
		t.Fatalf("create golden file: %v", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	fmt.Fprint(writer, "seed,tick,hash\n")

	cfg := DefaultConfig()
	for _, seed := range goldenSeeds {
		world := NewWorld(seed, cfg)
		for _, tick := range goldenTicks {
			Run(world, tick)
			fmt.Fprintf(writer, "%d,%d,%016x\n", seed, tick, Hash(world))
		}
	}

	if err := writer.Flush(); err != nil {
		t.Fatalf("write golden file: %v", err)
	}
	t.Logf("regenerated %s (config_hash %016x)", goldenPath, cfg.Hash())
}

func readGoldenFile(t *testing.T) []goldenRow {
	t.Helper()

	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file (regenerate with `make golden`): %v", err)
	}

	var rows []goldenRow
	for lineNumber, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "seed,") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 3 {
			t.Fatalf("golden line %d: expected 3 fields, got %d", lineNumber+1, len(fields))
		}
		seed, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			t.Fatalf("golden line %d: seed: %v", lineNumber+1, err)
		}
		tick, err := strconv.ParseInt(fields[1], 10, 32)
		if err != nil {
			t.Fatalf("golden line %d: tick: %v", lineNumber+1, err)
		}
		hash, err := strconv.ParseUint(fields[2], 16, 64)
		if err != nil {
			t.Fatalf("golden line %d: hash: %v", lineNumber+1, err)
		}
		rows = append(rows, goldenRow{seed: seed, tick: int32(tick), hash: hash})
	}
	return rows
}

// TestSubstreamIndependence proves the property that makes per-agent
// substreams worth having: one agent's draw sequence is a pure function of
// (seed, id, tick, purpose) and cannot be perturbed by which other agents
// happen to draw, in what order, or how many times.
//
// Agents do not exist yet, so this exercises AgentRand directly — which is the
// level the property actually lives at.
func TestSubstreamIndependence(t *testing.T) {
	const seed = uint64(0xDEADBEEFCAFEBABE)
	const subject = uint32(7)
	const tick = int32(4321)
	const draws = 16

	reference := make([]uint64, draws)
	isolated := AgentRand(seed, subject, tick, purposeWander)
	for i := range reference {
		reference[i] = isolated.Next()
	}

	// Same subject, interleaved with an arbitrary crowd of other agents that
	// draw an arbitrary number of times, in an arbitrary order.
	crowd := []uint32{5, 9, 100, 1, 6, 8, 4294967295}
	interleaved := AgentRand(seed, subject, tick, purposeWander)
	for i := 0; i < draws; i++ {
		for offset, other := range crowd {
			noise := AgentRand(seed, other, tick, purposeWander)
			for j := 0; j <= offset+i; j++ {
				noise.Next()
			}
		}
		if got := interleaved.Next(); got != reference[i] {
			t.Fatalf("draw %d: agent %d got %016x with a crowd, %016x alone", i, subject, got, reference[i])
		}
	}

	// Different purposes on the same (seed, id, tick) must not correlate.
	wander := AgentRand(seed, subject, tick, purposeWander)
	tiebreak := AgentRand(seed, subject, tick, purposeTiebreak)
	if wander.Next() == tiebreak.Next() {
		t.Error("purposeWander and purposeTiebreak produced the same first draw")
	}

	// Neighbouring ticks and neighbouring ids must not alias either.
	current := AgentRand(seed, subject, tick, purposeWander)
	nextTick := AgentRand(seed, subject, tick+1, purposeWander)
	nextAgent := AgentRand(seed, subject+1, tick, purposeWander)
	first := current.Next()
	if first == nextTick.Next() {
		t.Error("tick and tick+1 produced the same first draw")
	}
	if first == nextAgent.Next() {
		t.Error("agent id and id+1 produced the same first draw")
	}
}

// TestIntnStaysInRangeAndIsRoughlyUniform is a sanity check on the Lemire
// bounded draw, including the awkward small-n cases.
func TestIntnStaysInRangeAndIsRoughlyUniform(t *testing.T) {
	for _, n := range []uint32{1, 2, 3, 7, 8, 625, 65536} {
		buckets := make([]int, n)
		random := AgentRand(42, 1, 0, purposeTiebreak)
		const draws = 100000
		for i := 0; i < draws; i++ {
			value := random.Intn(n)
			if value >= n {
				t.Fatalf("Intn(%d) returned %d, out of range", n, value)
			}
			buckets[value]++
		}
		if n <= 8 {
			expected := draws / int(n)
			for value, count := range buckets {
				if count < expected/2 || count > expected*2 {
					t.Errorf("Intn(%d): bucket %d got %d draws, expected around %d", n, value, count, expected)
				}
			}
		}
	}

	random := AgentRand(1, 1, 0, purposeWander)
	if got := random.Intn(0); got != 0 {
		t.Errorf("Intn(0) = %d, want 0", got)
	}
}
