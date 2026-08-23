package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realBenchOutput is a verbatim run of the CI command, header block included,
// so the parser is exercised against the shape it actually reads rather than
// an invented one.
const realBenchOutput = `date: 2026-08-23T21:14:07Z
commit: 2f03d46
runner: Linux X64
cpus: 4
go: go version go1.25.0 linux/amd64

goos: linux
goarch: amd64
pkg: github.com/chunlea/marionette/pkg/server/core
cpu: AMD EPYC 7763 64-Core Processor
BenchmarkTaskDispatch_CreateToAssigned-4         	  214899	      5094 ns/op	    4587 B/op	      46 allocs/op
BenchmarkTaskDispatch_DispatchNext-4             	  314344	      3741 ns/op	    2613 B/op	      28 allocs/op
BenchmarkTaskDispatch_NoWorkToDo-4               	 9944290	       115.3 ns/op	     200 B/op	       7 allocs/op
BenchmarkRedispatchPass_Empty/sessions=1-4       	 5702319	       210.5 ns/op	     664 B/op	       7 allocs/op
BenchmarkRedispatchPass_Empty/sessions=50-4      	  190558	      6261 ns/op	   28512 B/op	     203 allocs/op
BenchmarkPermission_RoundTrip-4                  	 1000000	      1121 ns/op	    1759 B/op	      17 allocs/op
PASS
ok  	github.com/chunlea/marionette/pkg/server/core	12.345s
`

func TestParseBench(t *testing.T) {
	got, err := ParseBench(strings.NewReader(realBenchOutput))
	require.NoError(t, err)

	require.Len(t, got, 6, "the header block and the PASS/ok lines must not parse as results")
	assert.Equal(t, "TaskDispatch_CreateToAssigned", got[0].Name, "the Benchmark prefix and the -N suffix are stripped")
	assert.InDelta(t, 5094, got[0].NsPer, 0.001)

	// A sub-benchmark keeps its name; only the GOMAXPROCS suffix goes.
	assert.Equal(t, "RedispatchPass_Empty/sessions=1", got[3].Name)
	assert.InDelta(t, 210.5, got[3].NsPer, 0.001, "fractional ns/op survives")
}

func TestParseBench_TakesTheMedianOfRepeats(t *testing.T) {
	// -count 3 where the first measurement is the cold outlier. A mean would
	// report 4600; the median reports the steady state.
	const out = `BenchmarkX-8	100	14000 ns/op	1 B/op	1 allocs/op
BenchmarkX-8	100	5000 ns/op	1 B/op	1 allocs/op
BenchmarkX-8	100	4800 ns/op	1 B/op	1 allocs/op
`
	got, err := ParseBench(strings.NewReader(out))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.InDelta(t, 5000, got[0].NsPer, 0.001)
}

func TestParseBench_EvenNumberOfRepeats(t *testing.T) {
	const out = `BenchmarkX-8	100	100 ns/op
BenchmarkX-8	100	200 ns/op
`
	got, err := ParseBench(strings.NewReader(out))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.InDelta(t, 150, got[0].NsPer, 0.001)
}

func TestParseBench_UnparseableFigureIsSkipped(t *testing.T) {
	const out = `BenchmarkGood-8	100	120 ns/op
BenchmarkBad-8	100	1.2.3 ns/op
`
	got, err := ParseBench(strings.NewReader(out))
	require.NoError(t, err)
	require.Len(t, got, 1, "one bad line must not lose the rest of the run")
	assert.Equal(t, "Good", got[0].Name)
}

func TestParseBench_NoResults(t *testing.T) {
	got, err := ParseBench(strings.NewReader("testing: warning: no tests to run\nPASS\n"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestParseBaseline_RealFile reads the committed baseline. This is the test
// that fails when someone reformats BASELINE.md in a way the gate cannot read
// — which is exactly when a silent gate would start comparing nothing.
func TestParseBaseline_RealFile(t *testing.T) {
	f, err := os.Open("../BASELINE.md")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	got, err := ParseBaseline(f)
	require.NoError(t, err)

	byName := make(map[string]float64, len(got))
	for _, r := range got {
		byName[r.Name] = r.NsPer
	}

	// One from each table, so both column layouts stay covered.
	assert.InDelta(t, 5095, byName["TaskDispatch_CreateToAssigned"], 0.001)
	assert.InDelta(t, 6102, byName["RedispatchPass_Empty/sessions=50"], 0.001, "thousands separators are stripped")
	assert.InDelta(t, 107381, byName["Store_GetSession"], 0.001, "the store table's extra µs/op column does not shift ns/op")
	assert.InDelta(t, 976612, byName["Store_CreateLogs/batch=100"], 0.001)

	assert.GreaterOrEqual(t, len(got), 14, "every recorded benchmark should parse")
}

func TestParseBaseline_LocatesNsPerColumnFromTheHeader(t *testing.T) {
	const md = "| Benchmark | ns/op | B/op |\n" +
		"|---|---:|---:|\n" +
		"| `A` | 1,000 | 8 |\n" +
		"\n" +
		"| Benchmark | ns/op | µs/op | B/op |\n" +
		"|---|---:|---:|---:|\n" +
		"| `B` | 2,500 | 2 | 16 |\n"

	got, err := ParseBaseline(strings.NewReader(md))
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.InDelta(t, 1000, got[0].NsPer, 0.001)
	assert.InDelta(t, 2500, got[1].NsPer, 0.001, "ns/op is read by header name, not by position")
}

func TestParseBaseline_IgnoresProse(t *testing.T) {
	const md = "Some prose about `TaskDispatch_NoWorkToDo` being cheap.\n" +
		"\n" +
		"| Benchmark | ns/op |\n" +
		"|---|---:|\n" +
		"| `A` | 100 |\n" +
		"| `B` | not recorded |\n"

	got, err := ParseBaseline(strings.NewReader(md))
	require.NoError(t, err)
	require.Len(t, got, 1, "a backticked name in prose is not a row, and an unparseable figure is skipped")
	assert.Equal(t, "A", got[0].Name)
}

// TestParseBaseline_DoesNotCarryColumnsBetweenTables covers the shape
// BASELINE.md actually has: the gate section's calibration table names the
// same benchmarks in backticks but has no ns/op column at all.
func TestParseBaseline_DoesNotCarryColumnsBetweenTables(t *testing.T) {
	const md = "| Benchmark | ns/op |\n" +
		"|---|---:|\n" +
		"| `Real` | 100 |\n" +
		"\n" +
		"| Benchmark | baseline | worst run |\n" +
		"|---|---:|---:|\n" +
		"| `Calibration` | 5,095 | 14,524 |\n"

	got, err := ParseBaseline(strings.NewReader(md))
	require.NoError(t, err)
	require.Len(t, got, 1, "a table with no ns/op column contributes nothing")
	assert.Equal(t, "Real", got[0].Name)
}

func TestParseBaseline_ShortRow(t *testing.T) {
	const md = "| Benchmark | B/op | ns/op |\n" +
		"|---|---:|---:|\n" +
		"| `Truncated` | 8 |\n" +
		"| `Whole` | 8 | 250 |\n"

	got, err := ParseBaseline(strings.NewReader(md))
	require.NoError(t, err)
	require.Len(t, got, 1, "a row too short to hold the ns/op column is skipped, not misread")
	assert.Equal(t, "Whole", got[0].Name)
	assert.InDelta(t, 250, got[0].NsPer, 0.001)
}

func TestParseBaseline_NoTable(t *testing.T) {
	got, err := ParseBaseline(strings.NewReader("# Performance baseline\n\nNothing recorded yet.\n"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCompare(t *testing.T) {
	baseline := []Result{
		{Name: "Fast", NsPer: 100},
		{Name: "Slow", NsPer: 100},
		{Name: "Same", NsPer: 100},
		{Name: "Skipped", NsPer: 100},
	}
	observed := []Result{
		{Name: "Fast", NsPer: 50},
		{Name: "Slow", NsPer: 300},
		{Name: "Same", NsPer: 100},
		{Name: "Brand new", NsPer: 42},
	}

	report := Compare(baseline, observed)

	require.Len(t, report.Compared, 3)
	assert.Equal(t, "Slow", report.Compared[0].Name, "worst ratio first")
	assert.InDelta(t, 3.0, report.Compared[0].Ratio(), 0.001)
	assert.InDelta(t, 0.5, report.Compared[2].Ratio(), 0.001, "an improvement is a ratio below 1")

	assert.Equal(t, []string{"Skipped"}, report.NotMeasured)
	assert.Equal(t, []string{"Brand new"}, report.NoBaseline)
}

func TestReport_Regressions(t *testing.T) {
	report := Compare(
		[]Result{{Name: "A", NsPer: 100}, {Name: "B", NsPer: 100}, {Name: "C", NsPer: 100}},
		[]Result{{Name: "A", NsPer: 199}, {Name: "B", NsPer: 200}, {Name: "C", NsPer: 201}},
	)

	over := report.Regressions(2.0)
	require.Len(t, over, 2, "the threshold is inclusive: exactly 2x counts")
	assert.Equal(t, "C", over[0].Name)
	assert.Equal(t, "B", over[1].Name)

	assert.Empty(t, report.Regressions(3.0))
}

func TestComparison_RatioWithZeroBaseline(t *testing.T) {
	assert.Zero(t, Comparison{Observed: 100}.Ratio(), "a zero baseline cannot produce a ratio")
}

func TestRun_MissingBaselineSkipsInsteadOfFailing(t *testing.T) {
	err := run("testdata/does-not-exist.md", benchFile(t, realBenchOutput), 2.0, true)
	assert.NoError(t, err, "an absent baseline must skip cleanly even with the gate on")
}

func TestRun_EmptyBaselineSkips(t *testing.T) {
	empty := benchFile(t, "# Performance baseline\n\nNothing recorded yet.\n")
	assert.NoError(t, run(empty, benchFile(t, realBenchOutput), 2.0, true))
}

func TestRun_EmptyBenchOutputSkips(t *testing.T) {
	assert.NoError(t, run("../BASELINE.md", benchFile(t, "PASS\n"), 2.0, true))
}

func TestRun_GateFiresOnRegression(t *testing.T) {
	const slow = `BenchmarkTaskDispatch_CreateToAssigned-4	100	50940 ns/op
`
	err := run("../BASELINE.md", benchFile(t, slow), 2.0, true)
	require.Error(t, err)

	var reg *regressionError
	require.ErrorAs(t, err, &reg, "a regression must be distinguishable from a tool failure")
	assert.Equal(t, 1, reg.count)
}

func TestRun_GateOffReportsWithoutFailing(t *testing.T) {
	const slow = `BenchmarkTaskDispatch_CreateToAssigned-4	100	50940 ns/op
`
	assert.NoError(t, run("../BASELINE.md", benchFile(t, slow), 2.0, false),
		"report-only is the CI mode: it must never fail the job")
}

func TestRun_PassesWhenWithinThreshold(t *testing.T) {
	assert.NoError(t, run("../BASELINE.md", benchFile(t, realBenchOutput), 2.0, true))
}

func TestRun_UnreadableBaselineIsAToolError(t *testing.T) {
	// A directory opens fine and fails on read, so it exercises the branch a
	// missing file skips past.
	err := run(t.TempDir(), benchFile(t, realBenchOutput), 2.0, false)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "over the threshold")
}

func TestRegressionError_CountsWhatFired(t *testing.T) {
	err := &regressionError{count: 3}
	assert.Equal(t, "benchcmp: 3 benchmark(s) over the threshold", err.Error())
}

func TestRun_UnreadableBenchFileIsAToolError(t *testing.T) {
	err := run("../BASELINE.md", "testdata/does-not-exist.txt", 2.0, false)
	require.Error(t, err)

	var reg *regressionError
	assert.NotErrorAs(t, err, &reg, "a missing input is a tool failure, not a verdict")
}

// benchFile writes content to a temp file and returns its path.
func benchFile(t *testing.T, content string) string {
	t.Helper()

	path := t.TempDir() + "/bench.txt"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
