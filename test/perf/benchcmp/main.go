package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// exitRegression is returned when gating is on and something is over the
// threshold. It is distinct from 1 so a genuine tool failure (an unreadable
// file, a malformed flag) is never read as a performance verdict.
//
// The distinction only survives when the built binary runs: `go run` reports
// any non-zero child status as 1. Both callers today (make bench-gate, the CI
// bench job) only need zero versus non-zero.
const exitRegression = 2

func main() {
	var (
		baselinePath = flag.String("baseline", "test/perf/BASELINE.md", "recorded numbers to compare against")
		benchPath    = flag.String("bench", "", "`go test -bench` output to read (default: stdin)")
		threshold    = flag.Float64("threshold", 2.0, "ratio at which a benchmark counts as a regression")
		gate         = flag.Bool("gate", false, "exit non-zero when a benchmark is at or over the threshold")
	)
	flag.Parse()

	if err := run(*baselinePath, *benchPath, *threshold, *gate); err != nil {
		var reg *regressionError
		if errors.As(err, &reg) {
			fmt.Fprintln(os.Stderr, reg.Error())
			os.Exit(exitRegression)
		}
		fmt.Fprintf(os.Stderr, "benchcmp: %v\n", err)
		os.Exit(1)
	}
}

// regressionError signals the gate fired, as opposed to the tool failing.
type regressionError struct{ count int }

func (e *regressionError) Error() string {
	return fmt.Sprintf("benchcmp: %d benchmark(s) over the threshold", e.count)
}

func run(baselinePath, benchPath string, threshold float64, gate bool) error {
	baselineFile, err := os.Open(baselinePath)
	if err != nil {
		// A missing baseline is not a failure. It is the state a new
		// checkout or a renamed file is in, and a gate that fails on it
		// teaches people to disable the gate.
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Printf("SKIP: no baseline at %s, nothing to compare against\n", baselinePath)
			return nil
		}
		return fmt.Errorf("open baseline: %w", err)
	}
	defer func() { _ = baselineFile.Close() }()

	baseline, err := ParseBaseline(baselineFile)
	if err != nil {
		return err
	}
	if len(baseline) == 0 {
		fmt.Printf("SKIP: %s records no benchmark numbers, nothing to compare against\n", baselinePath)
		return nil
	}

	benchIn := os.Stdin
	if benchPath != "" {
		f, err := os.Open(benchPath)
		if err != nil {
			return fmt.Errorf("open benchmark output: %w", err)
		}
		defer func() { _ = f.Close() }()
		benchIn = f
	}

	observed, err := ParseBench(benchIn)
	if err != nil {
		return err
	}
	if len(observed) == 0 {
		fmt.Println("SKIP: the benchmark output has no results, nothing to compare")
		return nil
	}

	report := Compare(baseline, observed)
	printReport(report, threshold, gate)

	if gate {
		if over := report.Regressions(threshold); len(over) > 0 {
			return &regressionError{count: len(over)}
		}
	}
	return nil
}

func printReport(report Report, threshold float64, gate bool) {
	mode := "report only, gate off"
	if gate {
		mode = fmt.Sprintf("gate on at %.2gx", threshold)
	}
	fmt.Printf("Benchmark vs test/perf/BASELINE.md (%s)\n\n", mode)

	if len(report.Compared) > 0 {
		width := len("Benchmark")
		for _, c := range report.Compared {
			if len(c.Name) > width {
				width = len(c.Name)
			}
		}
		fmt.Printf("  %-*s  %12s  %12s  %8s\n", width, "Benchmark", "baseline", "observed", "ratio")
		fmt.Printf("  %s  %s  %s  %s\n", strings.Repeat("-", width), strings.Repeat("-", 12), strings.Repeat("-", 12), strings.Repeat("-", 8))
		for _, c := range report.Compared {
			marker := ""
			if c.Ratio() >= threshold {
				marker = "  <-- over threshold"
			}
			fmt.Printf("  %-*s  %12.0f  %12.0f  %7.2fx%s\n", width, c.Name, c.Baseline, c.Observed, c.Ratio(), marker)
		}
		fmt.Println()
	}

	if len(report.NotMeasured) > 0 {
		fmt.Printf("  not measured by this run (%d): %s\n", len(report.NotMeasured), strings.Join(report.NotMeasured, ", "))
	}
	if len(report.NoBaseline) > 0 {
		fmt.Printf("  no baseline recorded (%d): %s\n", len(report.NoBaseline), strings.Join(report.NoBaseline, ", "))
	}
	if len(report.NotMeasured) > 0 || len(report.NoBaseline) > 0 {
		fmt.Println()
	}

	over := report.Regressions(threshold)
	switch {
	case len(over) == 0:
		fmt.Printf("OK: %d benchmark(s) compared, none at or over %.2gx\n", len(report.Compared), threshold)
	case gate:
		fmt.Printf("FAIL: %d benchmark(s) at or over %.2gx\n", len(over), threshold)
	default:
		fmt.Printf("NOTE: %d benchmark(s) at or over %.2gx. Not failing: see the bench job comment in .github/workflows/ci.yml\n", len(over), threshold)
	}
}
