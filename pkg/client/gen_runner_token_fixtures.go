//go:build ignore

// Command gen_runner_token_fixtures regenerates the runner-token response
// fixtures used by admin_runner_token_decode_test.go.
//
// The fixtures are built from the server's own response types
// (admin.CreateRunnerTokenResponse and admin.RotateRunnerTokenResponse) rather
// than written by hand, so a change to the wire shape shows up as a fixture
// diff instead of quietly leaving the tests asserting a shape the server no
// longer sends. Hand-written fixtures are what let the flat-vs-nested drift go
// unnoticed until it was found live.
//
// Regenerate with:
//
//	go generate ./pkg/client/...
//
// Values here are fixed, never random: a regenerated fixture must differ only
// when the shape actually changes.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chunlea/marionette/pkg/server/admin"
	"github.com/chunlea/marionette/pkg/store"
)

func main() {
	created := time.Date(2026, 8, 23, 11, 4, 5, 0, time.UTC)
	expires := created.Add(720 * time.Hour)
	deadline := created.Add(24 * time.Hour)
	runnerID := "run_0002xK9mNpV1StGXR8"
	createdBy := "admin"

	token := &store.RunnerToken{
		ID:          "rtok_0002xK9mNqW2TuHYS9",
		TokenPrefix: "rtok_8Kd2mVp1",
		HashVersion: 1,
		RunnerID:    &runnerID,
		PoolName:    "gpu-pool",
		Status:      "active",
		// Present so the fixture proves the server withholds it (json:"-").
		TokenHash:        "must-not-appear-in-json",
		RotationDeadline: &deadline,
		Labels:           json.RawMessage(`{"env":"prod","tier":"premium"}`),
		CreatedAt:        created,
		CreatedBy:        &createdBy,
		ExpiresAt:        &expires,
	}

	fixtures := map[string]any{
		"runner_token_create_response.json": admin.CreateRunnerTokenResponse{
			Token:    token,
			RawToken: "rtok_8Kd2mVp1SecretValueDoNotLog",
		},
		"runner_token_rotate_response.json": admin.RotateRunnerTokenResponse{
			Token:    token,
			RawToken: "rtok_9Ne3pWq2RotatedSecretValue",
		},
	}

	for name, payload := range fixtures {
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			fail("marshalling %s: %v", name, err)
		}

		path := filepath.Join("testdata", name)
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			fail("writing %s: %v", path, err)
		}
		fmt.Println("wrote", path)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
