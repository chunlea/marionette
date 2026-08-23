package main

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

// captureOutput runs fn with command output redirected into a buffer.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	buf := &bytes.Buffer{}
	original := getOutput
	getOutput = func() io.Writer { return buf }
	t.Cleanup(func() { getOutput = original })
	fn()
	return buf.String()
}

// The profile fields used to arrive as json.RawMessage and the guard here was
// a byte count — "longer than {}". They are decoded maps now, so a length is a
// count of entries, and the old test silently hid any profile with one or two
// resources, or with fewer than three labels.
func TestPrintProfileSectionShowsSmallCollections(t *testing.T) {
	out := captureOutput(t, func() {
		printProfileSection("Resources", map[string]any{"cpu": "2"})
	})
	assert.Contains(t, out, "Resources:")
	assert.Contains(t, out, `"cpu": "2"`)

	out = captureOutput(t, func() {
		printProfileSection("Labels", map[string]string{"env": "prod"})
	})
	assert.Contains(t, out, "Labels:")
	assert.Contains(t, out, `"env": "prod"`)

	out = captureOutput(t, func() {
		printProfileSection("Tunnels", []map[string]any{{"type": "http"}})
	})
	assert.Contains(t, out, "Tunnels:")
	assert.Contains(t, out, `"type": "http"`)
}

func TestPrintProfileSectionSkipsEmptyCollections(t *testing.T) {
	for name, run := range map[string]func(){
		"empty map":   func() { printProfileSection("Resources", map[string]any{}) },
		"nil map":     func() { printProfileSection("Resources", map[string]any(nil)) },
		"empty slice": func() { printProfileSection("Tunnels", []map[string]any{}) },
		"empty labels": func() {
			printProfileSection("Labels", map[string]string{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, captureOutput(t, run))
		})
	}
}
