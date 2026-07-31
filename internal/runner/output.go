package runner

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// canonicalColumns are the eleven fixed CSV columns, in the plan's order. Sweep
// axis columns are appended after them.
var canonicalColumns = []string{
	"seed", "outcome", "peak_pop", "peak_year", "extinct_year",
	"final_pop", "final_tick", "state_hash", "config_hash", "version", "wall_ms",
}

// SidecarPath is where the resolved base config is written next to an output
// file: runs.csv -> runs.csv.config.json.
//
// The suffix is appended rather than substituted so that two outputs whose
// names differ only by extension cannot collide on one sidecar.
func SidecarPath(out string) string { return out + ".config.json" }

// CellSummaryPath is where a sweep's per-cell aggregate is written:
// runs.csv -> runs.csv.cells.csv.
func CellSummaryPath(out string) string { return out + ".cells.csv" }

// SortRecords orders records by cell index, then seed. This is the ordering
// every writer emits in, and it is what makes an output file byte-identical
// however many workers produced it.
//
// SliceStable with a total order, per the repository convention: an unstable
// sort would be free to permute equal keys differently between runs.
func SortRecords(records []Record) {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].CellIndex != records[j].CellIndex {
			return records[i].CellIndex < records[j].CellIndex
		}
		return records[i].Seed < records[j].Seed
	})
}

// AxisNames returns the axis names of a batch, taken from the first record
// that has any. Every record of one batch carries the same axes in the same
// order, so one is enough.
func AxisNames(records []Record) []string {
	for _, record := range records {
		if len(record.Axes) == 0 {
			continue
		}
		names := make([]string, len(record.Axes))
		for i, axis := range record.Axes {
			names[i] = axis.Name
		}
		return names
	}
	return nil
}

// Write emits the per-run records to path, choosing the writer from the
// extension: .json selects JSON, anything else CSV.
//
// baseConfigHash is the hash of the resolved base config and appears in the
// JSON header. It is not necessarily any record's own hash: in a sweep each
// record carries the hash of its own cell.
func Write(path string, records []Record, version string, baseConfigHash uint64) error {
	sorted := make([]Record, len(records))
	copy(sorted, records)
	SortRecords(sorted)

	if strings.EqualFold(filepath.Ext(path), ".json") {
		return writeJSON(path, sorted, version, baseConfigHash)
	}
	return writeCSV(path, sorted)
}

func writeCSV(path string, records []Record) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer file.Close()

	// encoding/csv writes LF line endings unless UseCRLF is set, which is what
	// the output schema requires.
	writer := csv.NewWriter(file)

	header := append(append([]string{}, canonicalColumns...), AxisNames(records)...)
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	for _, record := range records {
		if err := writer.Write(csvRow(record)); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return file.Close()
}

func csvRow(record Record) []string {
	row := []string{
		strconv.FormatUint(record.Seed, 10),
		record.Outcome,
		strconv.Itoa(record.PeakPop),
		strconv.Itoa(record.PeakYear),
		strconv.Itoa(record.ExtinctYear),
		strconv.Itoa(record.FinalPop),
		strconv.Itoa(record.FinalTick),
		fmt.Sprintf("%016x", record.StateHash),
		fmt.Sprintf("%016x", record.ConfigHash),
		record.Version,
		strconv.FormatInt(record.WallMS, 10),
	}
	for _, axis := range record.Axes {
		row = append(row, axis.Value)
	}
	return row
}

// jsonDocument is the JSON output shape: a header carrying the values common to
// the whole batch, then the records.
type jsonDocument struct {
	Version    string       `json:"version"`
	ConfigHash string       `json:"config_hash"`
	Axes       []string     `json:"axes,omitempty"`
	Records    []jsonRecord `json:"records"`
}

// jsonRecord mirrors the CSV columns, including config_hash and version on
// every record: a consumer that filters the array must not have to carry the
// header along to know whether two records are comparable.
type jsonRecord struct {
	Seed        uint64            `json:"seed"`
	Outcome     string            `json:"outcome"`
	PeakPop     int               `json:"peak_pop"`
	PeakYear    int               `json:"peak_year"`
	ExtinctYear int               `json:"extinct_year"`
	FinalPop    int               `json:"final_pop"`
	FinalTick   int               `json:"final_tick"`
	StateHash   string            `json:"state_hash"`
	ConfigHash  string            `json:"config_hash"`
	Version     string            `json:"version"`
	WallMS      int64             `json:"wall_ms"`
	Cell        int               `json:"cell,omitempty"`
	AxisValues  map[string]string `json:"axis_values,omitempty"`
}

func writeJSON(path string, records []Record, version string, baseConfigHash uint64) error {
	document := jsonDocument{
		Version:    version,
		ConfigHash: fmt.Sprintf("%016x", baseConfigHash),
		Axes:       AxisNames(records),
		Records:    make([]jsonRecord, 0, len(records)),
	}

	for _, record := range records {
		entry := jsonRecord{
			Seed:        record.Seed,
			Outcome:     record.Outcome,
			PeakPop:     record.PeakPop,
			PeakYear:    record.PeakYear,
			ExtinctYear: record.ExtinctYear,
			FinalPop:    record.FinalPop,
			FinalTick:   record.FinalTick,
			StateHash:   fmt.Sprintf("%016x", record.StateHash),
			ConfigHash:  fmt.Sprintf("%016x", record.ConfigHash),
			Version:     record.Version,
			WallMS:      record.WallMS,
			Cell:        record.CellIndex,
		}
		if len(record.Axes) > 0 {
			// encoding/json sorts map keys, so this stays byte-stable.
			entry.AxisValues = make(map[string]string, len(record.Axes))
			for _, axis := range record.Axes {
				entry.AxisValues[axis.Name] = axis.Value
			}
		}
		document.Records = append(document.Records, entry)
	}

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// CellSummary aggregates every run of one sweep cell. It is what actually
// answers "which parameter region is interesting": the per-run file says what
// each seed did, this says what the cell does.
type CellSummary struct {
	Index      int
	Axes       []Axis
	Runs       int
	Outcomes   map[string]int
	MeanPeak   float64
	MedianPeak float64
	MeanFinal  float64
	// MedianFinal, like MedianPeak, is the average of the two middle values
	// when the count is even.
	MedianFinal float64
	ConfigHash  uint64
}

// summaryOutcomes is the fixed column order of the six outcome counts. It is a
// list rather than a map iteration so the aggregate's columns never move.
var summaryOutcomes = []string{
	"EXTINCT", "STABLE", "OSCILLATING", "OVERRUN", "DECLINING", "TIMEOUT",
}

// SummariseCells groups records by cell and aggregates each group. Records may
// arrive in any order; the result is ordered by cell index.
func SummariseCells(records []Record) []CellSummary {
	sorted := make([]Record, len(records))
	copy(sorted, records)
	SortRecords(sorted)

	summaries := make([]CellSummary, 0)
	for start := 0; start < len(sorted); {
		end := start
		for end < len(sorted) && sorted[end].CellIndex == sorted[start].CellIndex {
			end++
		}
		summaries = append(summaries, summariseGroup(sorted[start:end]))
		start = end
	}
	return summaries
}

func summariseGroup(group []Record) CellSummary {
	summary := CellSummary{
		Index:      group[0].CellIndex,
		Axes:       group[0].Axes,
		Runs:       len(group),
		Outcomes:   make(map[string]int, len(summaryOutcomes)),
		ConfigHash: group[0].ConfigHash,
	}

	peaks := make([]int, 0, len(group))
	finals := make([]int, 0, len(group))
	var peakSum, finalSum int64

	for _, record := range group {
		summary.Outcomes[record.Outcome]++
		peaks = append(peaks, record.PeakPop)
		finals = append(finals, record.FinalPop)
		peakSum += int64(record.PeakPop)
		finalSum += int64(record.FinalPop)
	}

	// The means are summed in integers and divided exactly once, so the
	// floating-point result cannot depend on the order the runs completed in.
	summary.MeanPeak = float64(peakSum) / float64(len(group))
	summary.MeanFinal = float64(finalSum) / float64(len(group))
	summary.MedianPeak = median(peaks)
	summary.MedianFinal = median(finals)

	return summary
}

func median(values []int) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := make([]int, len(values))
	copy(ordered, values)
	sort.Ints(ordered)

	middle := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return float64(ordered[middle])
	}
	return float64(ordered[middle-1]+ordered[middle]) / 2
}

// WriteCellSummary writes the per-cell aggregate CSV.
func WriteCellSummary(path string, records []Record) error {
	summaries := SummariseCells(records)
	axisNames := AxisNames(records)

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)

	header := append([]string{"cell"}, axisNames...)
	header = append(header, "runs")
	for _, outcome := range summaryOutcomes {
		header = append(header, strings.ToLower(outcome))
	}
	header = append(header,
		"mean_peak_pop", "median_peak_pop", "mean_final_pop", "median_final_pop", "config_hash")
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	for _, summary := range summaries {
		row := []string{strconv.Itoa(summary.Index)}
		for _, axis := range summary.Axes {
			row = append(row, axis.Value)
		}
		row = append(row, strconv.Itoa(summary.Runs))
		for _, outcome := range summaryOutcomes {
			row = append(row, strconv.Itoa(summary.Outcomes[outcome]))
		}
		row = append(row,
			formatMean(summary.MeanPeak),
			formatMean(summary.MedianPeak),
			formatMean(summary.MeanFinal),
			formatMean(summary.MedianFinal),
			fmt.Sprintf("%016x", summary.ConfigHash),
		)
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return file.Close()
}

// formatMean pins the aggregate's float formatting to one decimal place, so
// the file is byte-stable and a median of two adjacent integers is still
// readable.
func formatMean(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}

// OutcomeCounts tallies a batch by outcome, for the end-of-run summary line.
func OutcomeCounts(records []Record) map[string]int {
	counts := make(map[string]int, len(summaryOutcomes))
	for _, record := range records {
		counts[record.Outcome]++
	}
	return counts
}

// FormatOutcomeCounts renders a tally in the fixed outcome order, omitting the
// outcomes that did not occur.
func FormatOutcomeCounts(counts map[string]int) string {
	parts := make([]string, 0, len(summaryOutcomes))
	for _, outcome := range summaryOutcomes {
		if counts[outcome] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", outcome, counts[outcome]))
		}
	}
	// Anything the fixed list does not cover — RUNNING, or an outcome added
	// later — would otherwise vanish from the summary without a trace.
	extra := make([]string, 0)
	for outcome, count := range counts {
		if count > 0 && !knownOutcome(outcome) {
			extra = append(extra, fmt.Sprintf("%s=%d", outcome, count))
		}
	}
	sort.Strings(extra)

	parts = append(parts, extra...)
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}

func knownOutcome(name string) bool {
	for _, outcome := range summaryOutcomes {
		if outcome == name {
			return true
		}
	}
	return false
}
