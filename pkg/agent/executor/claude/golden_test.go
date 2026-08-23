package claude

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Golden recordings of real Claude Code CLI 2.1.241 output.
// See testdata/golden/README.md for how they were produced.
const (
	goldenBasic   = "basic.jsonl"
	goldenToolUse = "tooluse.jsonl"
	goldenResume  = "resume.jsonl"
)

// goldenLines returns every non-empty line of a golden recording.
// Tests read the recordings rather than embedding copies of them, so a
// re-recording immediately shows up as a test failure instead of drifting.
func goldenLines(t *testing.T, name string) [][]byte {
	t.Helper()

	f, err := os.Open(filepath.Join("testdata", "golden", name))
	require.NoError(t, err, "golden recording %s must exist", name)
	defer func() { _ = f.Close() }()

	var lines [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		lines = append(lines, append([]byte(nil), line...))
	}
	require.NoError(t, scanner.Err())
	require.NotEmpty(t, lines, "golden recording %s must not be empty", name)

	return lines
}

// goldenLineOfType returns the first line of the given message type. Pass a
// subtype to disambiguate: a recording carries several `system` lines and only
// one of them is the `init` line.
func goldenLineOfType(t *testing.T, name, msgType string, subtype ...string) []byte {
	t.Helper()

	for _, line := range goldenLines(t, name) {
		var msg StreamMessage
		if err := jsonUnmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Type != msgType {
			continue
		}
		if len(subtype) > 0 && msg.Subtype != subtype[0] {
			continue
		}
		return line
	}

	t.Fatalf("golden recording %s has no %q line (subtype %v)", name, msgType, subtype)
	return nil
}
