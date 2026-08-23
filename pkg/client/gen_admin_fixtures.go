//go:build ignore

// Command gen_admin_fixtures regenerates the admin response fixtures used by
// admin_runner_token_decode_test.go.
//
// The fixtures are built from the server's own response types in
// pkg/server/admin/admintypes rather than written by hand, so a change to the
// wire shape shows up as a fixture diff instead of quietly leaving the tests
// asserting a shape the server no longer sends. Hand-written fixtures are what
// let the flat-vs-nested drift go unnoticed until it was found live — twice:
// once in the runner token response, and once in the API key response, whose
// SDK decode had never worked at all.
//
// These fixtures pin the wire *shape*. That the server withholds secrets is
// proved separately, and closer to the source, by
// TestAdminResponsesWithholdSecrets in pkg/server/admin.
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

	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
)

func main() {
	created := time.Date(2026, 8, 23, 11, 4, 5, 0, time.UTC)
	expires := created.Add(720 * time.Hour)
	deadline := created.Add(24 * time.Hour)
	runnerID := "run_0002xK9mNpV1StGXR8"
	createdBy := "admin"

	token := &admintypes.RunnerToken{
		ID:               "rtok_0002xK9mNqW2TuHYS9",
		TokenPrefix:      "rtok_8Kd2mVp1",
		RunnerID:         &runnerID,
		PoolName:         "gpu-pool",
		Status:           "active",
		RotationDeadline: &deadline,
		Labels:           map[string]string{"env": "prod", "tier": "premium"},
		CreatedAt:        created,
		CreatedBy:        &createdBy,
		ExpiresAt:        &expires,
	}

	key := &admintypes.APIKey{
		ID:          "key_0002xK9mNrX3UvIZT0",
		Name:        "ci",
		KeyPrefix:   "mk_7Jc1lUo0",
		Scopes:      []string{"sessions:*", "tasks:*"},
		Labels:      map[string]string{"env": "prod"},
		Annotations: map[string]string{},
		CreatedAt:   created,
		CreatedBy:   &createdBy,
		ExpiresAt:   &expires,
	}

	fixtures := map[string]any{
		"runner_token_create_response.json": admintypes.CreatedRunnerToken{
			Token:    token,
			RawToken: "rtok_8Kd2mVp1SecretValueDoNotLog",
		},
		"runner_token_rotate_response.json": admintypes.CreatedRunnerToken{
			Token:    token,
			RawToken: "rtok_9Ne3pWq2RotatedSecretValue",
		},
		"api_key_create_response.json": admintypes.CreatedAPIKey{
			Key:      key,
			RawToken: "mk_7Jc1lUo0SecretValueDoNotLog",
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
