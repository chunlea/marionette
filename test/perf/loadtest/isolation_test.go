package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fakeExecutorPackage is the import path that must never appear in a shipped
// binary's dependency graph.
const fakeExecutorPackage = "github.com/chunlea/marionette/test/perf/fakeexec"

// TestProductionBinariesDoNotLinkTheFakeExecutor is the guard that makes the
// fake executor's placement a guarantee rather than a convention.
//
// The alternative the brief offered was a build tag inside pkg/agent. A tag is
// one typo in a Makefile or a CI matrix away from producing a binary that looks
// and starts exactly like the real agent and silently completes every task
// without running anything - a failure it would take a production incident to
// notice. This asks the real dependency graph instead, so the mistake is a
// failing test rather than a code review someone has to be paying attention
// during.
func TestProductionBinariesDoNotLinkTheFakeExecutor(t *testing.T) {
	for _, binary := range []string{"./cmd/server", "./cmd/agent", "./cmd/mctl"} {
		t.Run(binary, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-deps", binary)
			cmd.Dir = "../../.."

			out, err := cmd.Output()
			if err != nil {
				t.Skipf("could not list dependencies of %s: %v", binary, err)
			}

			for _, dep := range strings.Split(string(out), "\n") {
				if strings.TrimSpace(dep) == fakeExecutorPackage {
					t.Fatalf(
						"%s links %s: a shipped binary must not be able to fake task execution",
						binary, fakeExecutorPackage)
				}
			}
		})
	}
}

func TestPercentile(t *testing.T) {
	if got := percentile(nil, 95); got != 0 {
		t.Fatalf("an empty sample set has no percentile, got %v", got)
	}

	samples := sortDurations([]time.Duration{
		50 * time.Millisecond,
		10 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		20 * time.Millisecond,
	})

	cases := []struct {
		p    float64
		want time.Duration
	}{
		{50, 30 * time.Millisecond},
		{95, 50 * time.Millisecond},
		{100, 50 * time.Millisecond},
		// Nearest-rank rounds below the first sample down to it rather than
		// off the front of the slice.
		{1, 10 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := percentile(samples, tc.p); got != tc.want {
			t.Errorf("percentile(p%v) = %v, want %v", tc.p, got, tc.want)
		}
	}
}
