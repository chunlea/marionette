// Package main compares a `go test -bench` run against the numbers recorded in
// test/perf/BASELINE.md.
//
// BASELINE.md is the single source of truth for the recorded numbers, so this
// reads the markdown tables directly rather than a generated sidecar file that
// would drift from them.
package main

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Result is one benchmark's ns/op, from either side of the comparison.
type Result struct {
	Name  string
	NsPer float64
}

// Comparison is one benchmark measured against its baseline.
type Comparison struct {
	Name     string
	Baseline float64
	Observed float64
}

// Ratio is how much slower the observation is than the baseline. Below 1 means
// faster.
func (c Comparison) Ratio() float64 {
	if c.Baseline == 0 {
		return 0
	}
	return c.Observed / c.Baseline
}

// Report is the outcome of comparing one benchmark run against the baseline.
type Report struct {
	// Compared holds every benchmark present on both sides, worst ratio first.
	Compared []Comparison

	// NotMeasured names baseline entries the run did not produce. That is the
	// normal case for a partial run — the store suite skips itself without a
	// Docker daemon — so it is reported rather than treated as a failure.
	NotMeasured []string

	// NoBaseline names benchmarks the run produced that BASELINE.md does not
	// record. New hot paths land here until the baseline is re-recorded.
	NoBaseline []string
}

// Regressions returns the comparisons at or above threshold, worst first.
func (r Report) Regressions(threshold float64) []Comparison {
	var out []Comparison
	for _, c := range r.Compared {
		if c.Ratio() >= threshold {
			out = append(out, c)
		}
	}
	return out
}

// benchLine matches a line of `go test -bench` output:
//
//	BenchmarkRedispatchPass_Empty/sessions=50-16   190558   6261 ns/op ...
//
// The trailing -N is GOMAXPROCS, which is a property of the machine rather
// than of the benchmark, so it is stripped along with the Benchmark prefix to
// leave the name BASELINE.md records.
var benchLine = regexp.MustCompile(`^Benchmark(\S+?)(?:-\d+)?\s+\d+\s+([0-9.]+)\s+ns/op`)

// ParseBench reads `go test -bench` output and returns the median ns/op per
// benchmark.
//
// The median, not the mean: with -count>1 the first measurement in a process
// is routinely the slow one (cold caches, a b.N ramp that has not settled),
// and one such sample drags a mean far enough to matter at these magnitudes.
//
// Lines that are not benchmark results are ignored, so the machine-description
// header the CI job prepends to bench.txt passes through harmlessly.
func ParseBench(r io.Reader) ([]Result, error) {
	samples := make(map[string][]float64)
	var order []string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		m := benchLine.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		ns, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		name := m[1]
		if _, seen := samples[name]; !seen {
			order = append(order, name)
		}
		samples[name] = append(samples[name], ns)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read benchmark output: %w", err)
	}

	out := make([]Result, 0, len(order))
	for _, name := range order {
		out = append(out, Result{Name: name, NsPer: median(samples[name])})
	}
	return out, nil
}

// median returns the middle value of xs, averaging the middle pair when the
// count is even. xs must not be empty.
func median(xs []float64) float64 {
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// baselineRow matches a markdown table row whose first cell is a backticked
// benchmark name:
//
//	| `TaskDispatch_DispatchNext` | 3,812 | 2,616 | 28 |
var baselineRow = regexp.MustCompile("^\\|\\s*`([^`]+)`\\s*\\|(.*)$")

// ParseBaseline reads BASELINE.md and returns the ns/op it records.
//
// The file holds more than one results table and they do not share a column
// layout (the store table carries an extra µs/op column), so the ns/op column
// is located from each table's own header rather than assumed to be first.
func ParseBaseline(r io.Reader) ([]Result, error) {
	var (
		out    []Result
		nsCol  = -1
		seen   = make(map[string]bool)
		scan   = bufio.NewScanner(r)
		header = regexp.MustCompile(`^\|\s*Benchmark\s*\|`)
	)
	scan.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scan.Scan() {
		line := scan.Text()

		// A markdown table cannot span a blank line or a heading, so either
		// ends the table the current column index belongs to. Without this a
		// later table of a different shape would be read with a stale index
		// and report the wrong column as ns/op.
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			nsCol = -1
			continue
		}

		if header.MatchString(line) {
			nsCol = indexOfColumn(splitRow(line), "ns/op")
			continue
		}

		m := baselineRow.FindStringSubmatch(line)
		if m == nil || nsCol < 1 {
			continue
		}
		// splitRow over the whole line keeps the name in column 0, so the
		// header-derived index applies unchanged.
		cells := splitRow(line)
		if nsCol >= len(cells) {
			continue
		}
		ns, err := parseNumber(cells[nsCol])
		if err != nil {
			continue
		}
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Result{Name: name, NsPer: ns})
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	return out, nil
}

// splitRow splits a markdown table row into its cells, dropping the empty
// leading and trailing fields the outer pipes produce.
func splitRow(line string) []string {
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// indexOfColumn returns the index of the named header cell, or -1.
func indexOfColumn(cells []string, name string) int {
	for i, c := range cells {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}

// parseNumber reads a table figure, which is written with thousands separators
// (5,095) and occasionally a leading ~.
func parseNumber(s string) (float64, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "~"))
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, fmt.Errorf("empty figure")
	}
	return strconv.ParseFloat(s, 64)
}

// Compare matches a run against the baseline, worst regression first.
func Compare(baseline, observed []Result) Report {
	byName := make(map[string]float64, len(observed))
	for _, o := range observed {
		byName[o.Name] = o.NsPer
	}

	var report Report
	inBaseline := make(map[string]bool, len(baseline))
	for _, b := range baseline {
		inBaseline[b.Name] = true
		got, ok := byName[b.Name]
		if !ok {
			report.NotMeasured = append(report.NotMeasured, b.Name)
			continue
		}
		report.Compared = append(report.Compared, Comparison{
			Name:     b.Name,
			Baseline: b.NsPer,
			Observed: got,
		})
	}
	for _, o := range observed {
		if !inBaseline[o.Name] {
			report.NoBaseline = append(report.NoBaseline, o.Name)
		}
	}

	sort.SliceStable(report.Compared, func(i, j int) bool {
		return report.Compared[i].Ratio() > report.Compared[j].Ratio()
	})
	return report
}
