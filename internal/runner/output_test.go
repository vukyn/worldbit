package runner

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// csvRows parses CSV bytes, header included.
func csvRows(t *testing.T, data []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v\n%s", err, data)
	}
	return rows
}

// wallTimePattern matches the wall_ms column in both output formats: the tenth
// comma-separated field of a CSV row, and the JSON member.
var (
	csvWallTime  = regexp.MustCompile(`(?m)^((?:[^,\n]*,){10})[0-9]+`)
	jsonWallTime = regexp.MustCompile(`"wall_ms": [0-9]+`)
)

// blankWallTimes zeroes the one non-deterministic column so that two files can
// be compared byte for byte. Blanking it rather than dropping it from the
// schema keeps the comparison honest about every other column's position.
func blankWallTimes(t *testing.T, data []byte) []byte {
	t.Helper()
	blanked := csvWallTime.ReplaceAll(data, []byte("${1}0"))
	return jsonWallTime.ReplaceAll(blanked, []byte(`"wall_ms": 0`))
}

// TestCSVHasTheCanonicalColumnOrder pins the output schema. Anything consuming
// these files positionally breaks silently if a column moves, so the order is
// asserted literally rather than derived from the writer's own list.
func TestCSVHasTheCanonicalColumnOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.csv")
	records := []Record{{
		Seed: 7, Outcome: "STABLE", PeakPop: 2600, PeakYear: 6, ExtinctYear: -1,
		FinalPop: 700, FinalTick: 12000, StateHash: 0xabc, ConfigHash: 0xdef,
		Version: "0.1.0", WallMS: 350,
		Axes: []Axis{{Name: "InitFoodPerCell", Value: "3"}},
	}}
	if err := Write(path, records, "0.1.0", 0xdef); err != nil {
		t.Fatalf("write: %v", err)
	}

	rows := csvRows(t, readFile(t, path))
	wantHeader := []string{
		"seed", "outcome", "peak_pop", "peak_year", "extinct_year",
		"final_pop", "final_tick", "state_hash", "config_hash", "version", "wall_ms",
		"InitFoodPerCell",
	}
	if strings.Join(rows[0], ",") != strings.Join(wantHeader, ",") {
		t.Errorf("header %v, want %v", rows[0], wantHeader)
	}

	wantRow := []string{
		"7", "STABLE", "2600", "6", "-1", "700", "12000",
		"0000000000000abc", "0000000000000def", "0.1.0", "350", "3",
	}
	if strings.Join(rows[1], ",") != strings.Join(wantRow, ",") {
		t.Errorf("row %v, want %v", rows[1], wantRow)
	}
}

// TestCSVUsesLineFeedEndings guards the one formatting detail encoding/csv
// would change behind a single field: CRLF endings would make every file
// differ from every previously recorded result.
func TestCSVUsesLineFeedEndings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.csv")
	if err := Write(path, []Record{{Seed: 1, Outcome: "STABLE"}}, "test", 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	if bytes.Contains(readFile(t, path), []byte("\r")) {
		t.Error("output contains a carriage return; the schema is LF-only")
	}
}

// TestJSONWriterSelectedByExtension covers the format dispatch and the header
// that makes a JSON result set self-describing.
func TestJSONWriterSelectedByExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.json")
	records := []Record{
		{Seed: 2, Outcome: "STABLE", ConfigHash: 0x22, Version: "0.1.0", CellIndex: 1,
			Axes: []Axis{{Name: "FoodMax", Value: "5"}}},
		{Seed: 1, Outcome: "EXTINCT", ConfigHash: 0x11, Version: "0.1.0", CellIndex: 0,
			Axes: []Axis{{Name: "FoodMax", Value: "2"}}},
	}
	if err := Write(path, records, "0.1.0", 0x99); err != nil {
		t.Fatalf("write: %v", err)
	}

	var document struct {
		Version    string   `json:"version"`
		ConfigHash string   `json:"config_hash"`
		Axes       []string `json:"axes"`
		Records    []struct {
			Seed       uint64            `json:"seed"`
			ConfigHash string            `json:"config_hash"`
			AxisValues map[string]string `json:"axis_values"`
		} `json:"records"`
	}
	if err := json.Unmarshal(readFile(t, path), &document); err != nil {
		t.Fatalf("parse json: %v", err)
	}

	if document.ConfigHash != "0000000000000099" {
		t.Errorf("header config_hash %q, want the base config's", document.ConfigHash)
	}
	if len(document.Axes) != 1 || document.Axes[0] != "FoodMax" {
		t.Errorf("header axes %v, want [FoodMax]", document.Axes)
	}
	if len(document.Records) != 2 || document.Records[0].Seed != 1 {
		t.Fatalf("records not sorted by cell then seed: %+v", document.Records)
	}
	// Each row carries its OWN config hash, not the base one: in a sweep the
	// two differ, and a consumer filtering the array must be able to tell
	// which records are comparable without carrying the header along.
	if document.Records[0].ConfigHash != "0000000000000011" {
		t.Errorf("record config_hash %q, want the cell's own", document.Records[0].ConfigHash)
	}
	if document.Records[0].AxisValues["FoodMax"] != "2" {
		t.Errorf("record axis values %v, want FoodMax=2", document.Records[0].AxisValues)
	}
}

// TestSummariseCellsCountsOutcomes checks the aggregate against hand-built
// records: the aggregate is what a sweep is read from, so an off-by-one in the
// grouping would misdirect every conclusion drawn from it.
func TestSummariseCellsCountsOutcomes(t *testing.T) {
	records := []Record{
		// Cell 0: three runs, peaks 100/300/200, finals 10/30/20.
		{CellIndex: 0, Seed: 1, Outcome: "STABLE", PeakPop: 100, FinalPop: 10, ConfigHash: 0xaa,
			Axes: []Axis{{Name: "FoodMax", Value: "2"}}},
		{CellIndex: 0, Seed: 2, Outcome: "EXTINCT", PeakPop: 300, FinalPop: 30, ConfigHash: 0xaa,
			Axes: []Axis{{Name: "FoodMax", Value: "2"}}},
		{CellIndex: 0, Seed: 3, Outcome: "STABLE", PeakPop: 200, FinalPop: 20, ConfigHash: 0xaa,
			Axes: []Axis{{Name: "FoodMax", Value: "2"}}},
		// Cell 1: two runs, so both medians are the mean of two values.
		{CellIndex: 1, Seed: 1, Outcome: "OVERRUN", PeakPop: 6554, FinalPop: 6554, ConfigHash: 0xbb,
			Axes: []Axis{{Name: "FoodMax", Value: "5"}}},
		{CellIndex: 1, Seed: 2, Outcome: "TIMEOUT", PeakPop: 1000, FinalPop: 500, ConfigHash: 0xbb,
			Axes: []Axis{{Name: "FoodMax", Value: "5"}}},
	}

	summaries := SummariseCells(records)
	if len(summaries) != 2 {
		t.Fatalf("summarised %d cells, want 2", len(summaries))
	}

	first := summaries[0]
	if first.Runs != 3 {
		t.Errorf("cell 0 runs %d, want 3", first.Runs)
	}
	if first.Outcomes["STABLE"] != 2 || first.Outcomes["EXTINCT"] != 1 {
		t.Errorf("cell 0 outcomes %v, want STABLE=2 EXTINCT=1", first.Outcomes)
	}
	if first.MeanPeak != 200 || first.MedianPeak != 200 {
		t.Errorf("cell 0 peak mean/median %v/%v, want 200/200", first.MeanPeak, first.MedianPeak)
	}
	if first.MeanFinal != 20 || first.MedianFinal != 20 {
		t.Errorf("cell 0 final mean/median %v/%v, want 20/20", first.MeanFinal, first.MedianFinal)
	}
	if first.ConfigHash != 0xaa {
		t.Errorf("cell 0 config hash %x, want aa", first.ConfigHash)
	}

	second := summaries[1]
	if second.Outcomes["OVERRUN"] != 1 || second.Outcomes["TIMEOUT"] != 1 {
		t.Errorf("cell 1 outcomes %v, want OVERRUN=1 TIMEOUT=1", second.Outcomes)
	}
	if want := float64(6554+1000) / 2; second.MeanPeak != want || second.MedianPeak != want {
		t.Errorf("cell 1 peak mean/median %v/%v, want %v", second.MeanPeak, second.MedianPeak, want)
	}
}

// TestWriteCellSummaryColumns pins the aggregate's schema, including that the
// six outcome counts are always present in a fixed order — a cell where an
// outcome did not occur must show a zero, not a missing column.
func TestWriteCellSummaryColumns(t *testing.T) {
	records := []Record{
		{CellIndex: 0, Seed: 1, Outcome: "STABLE", PeakPop: 100, FinalPop: 50, ConfigHash: 0xaa,
			Axes: []Axis{{Name: "InitFoodPerCell", Value: "1"}, {Name: "ReproEnergyCost", Value: "40"}}},
		{CellIndex: 0, Seed: 2, Outcome: "DECLINING", PeakPop: 200, FinalPop: 30, ConfigHash: 0xaa,
			Axes: []Axis{{Name: "InitFoodPerCell", Value: "1"}, {Name: "ReproEnergyCost", Value: "40"}}},
	}

	path := filepath.Join(t.TempDir(), "runs.csv.cells.csv")
	if err := WriteCellSummary(path, records); err != nil {
		t.Fatalf("write: %v", err)
	}

	rows := csvRows(t, readFile(t, path))
	wantHeader := []string{
		"cell", "InitFoodPerCell", "ReproEnergyCost", "runs",
		"extinct", "stable", "oscillating", "overrun", "declining", "timeout",
		"mean_peak_pop", "median_peak_pop", "mean_final_pop", "median_final_pop", "config_hash",
	}
	if strings.Join(rows[0], ",") != strings.Join(wantHeader, ",") {
		t.Fatalf("header %v,\nwant %v", rows[0], wantHeader)
	}

	wantRow := []string{
		"0", "1", "40", "2",
		"0", "1", "0", "0", "1", "0",
		"150.0", "150.0", "40.0", "40.0", "00000000000000aa",
	}
	if strings.Join(rows[1], ",") != strings.Join(wantRow, ",") {
		t.Errorf("row %v,\nwant %v", rows[1], wantRow)
	}
}

// TestSidecarAndSummaryPaths pins the naming convention, which several
// downstream steps and the README depend on.
func TestSidecarAndSummaryPaths(t *testing.T) {
	if got, want := SidecarPath("out/runs.csv"), "out/runs.csv.config.json"; got != want {
		t.Errorf("SidecarPath = %q, want %q", got, want)
	}
	if got, want := CellSummaryPath("out/runs.json"), "out/runs.json.cells.csv"; got != want {
		t.Errorf("CellSummaryPath = %q, want %q", got, want)
	}
}

// TestFormatOutcomeCountsIsOrdered keeps the end-of-run summary line stable:
// tallying through a map and printing it in iteration order would reshuffle the
// line on every run.
func TestFormatOutcomeCountsIsOrdered(t *testing.T) {
	counts := map[string]int{"TIMEOUT": 3, "EXTINCT": 1, "STABLE": 2, "RUNNING": 1}
	want := "EXTINCT=1 STABLE=2 TIMEOUT=3 RUNNING=1"
	for attempt := 0; attempt < 8; attempt++ {
		if got := FormatOutcomeCounts(counts); got != want {
			t.Fatalf("FormatOutcomeCounts = %q, want %q", got, want)
		}
	}
	if got := FormatOutcomeCounts(map[string]int{}); got != "none" {
		t.Errorf("empty tally rendered %q, want %q", got, "none")
	}
}

// TestAggregatesHandleAnEmptyBatch keeps the aggregate paths from panicking on
// a batch that produced nothing — a sweep whose every cell was skipped reaches
// here before the caller's own guard would.
func TestAggregatesHandleAnEmptyBatch(t *testing.T) {
	if got := SummariseCells(nil); len(got) != 0 {
		t.Errorf("SummariseCells(nil) = %v, want no summaries", got)
	}
	if got := AxisNames(nil); got != nil {
		t.Errorf("AxisNames(nil) = %v, want nil", got)
	}
	if got := median(nil); got != 0 {
		t.Errorf("median(nil) = %v, want 0", got)
	}

	path := filepath.Join(t.TempDir(), "empty.csv.cells.csv")
	if err := WriteCellSummary(path, nil); err != nil {
		t.Fatalf("write empty summary: %v", err)
	}
	if rows := csvRows(t, readFile(t, path)); len(rows) != 1 {
		t.Errorf("empty summary has %d rows, want just the header", len(rows))
	}
}

// TestOutcomeCountsTallies covers the tally the end-of-run line is built from.
func TestOutcomeCountsTallies(t *testing.T) {
	counts := OutcomeCounts([]Record{
		{Outcome: "STABLE"}, {Outcome: "STABLE"}, {Outcome: "EXTINCT"},
	})
	if counts["STABLE"] != 2 || counts["EXTINCT"] != 1 || len(counts) != 2 {
		t.Errorf("OutcomeCounts = %v, want STABLE=2 EXTINCT=1", counts)
	}
}

// TestAxisNamesTolerateEmptyLeadingRecords covers a batch that mixes a plain
// cell with swept ones: the names must come from whichever record has them,
// not from whichever happens to be first.
func TestAxisNamesTolerateEmptyLeadingRecords(t *testing.T) {
	names := AxisNames([]Record{
		{Seed: 1},
		{Seed: 2, Axes: []Axis{{Name: "FoodMax", Value: "5"}}},
	})
	if len(names) != 1 || names[0] != "FoodMax" {
		t.Errorf("AxisNames = %v, want [FoodMax]", names)
	}
}
