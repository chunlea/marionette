package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:generate go run gen_admin_fixtures.go

// The admin API returns runner tokens as {"token": {...}, "raw_token": "..."}.
// RunnerTokenWithSecret flattens that, so it needs custom JSON methods; without
// them every embedded field decoded as its zero value and
// `mctl admin runner-tokens create` printed a blank ID, pool and prefix.
//
// The fixtures are generated from the server's own response types
// (admintypes.CreatedRunnerToken and admintypes.CreatedAPIKey) so they
// cannot drift into a hand-written guess of the shape. Regenerate them with
// `go generate ./pkg/client/...` (see gen_admin_fixtures.go).

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return data
}

func assertDecodedToken(t *testing.T, got *RunnerTokenWithSecret, wantRawToken string) {
	t.Helper()

	// These are exactly the fields the CLI prints.
	assert.Equal(t, "rtok_0002xK9mNqW2TuHYS9", got.ID, "ID must not be blank")
	assert.Equal(t, "gpu-pool", got.PoolName, "pool must not be blank")
	assert.Equal(t, "rtok_8Kd2mVp1", got.TokenPrefix, "prefix must not be blank")
	assert.Equal(t, "active", got.Status)
	assert.False(t, got.CreatedAt.IsZero(), "created must not be the zero time")
	assert.Equal(t,
		time.Date(2026, 8, 23, 11, 4, 5, 0, time.UTC),
		got.CreatedAt.UTC(),
	)

	require.NotNil(t, got.ExpiresAt)
	assert.Equal(t, time.Date(2026, 9, 22, 11, 4, 5, 0, time.UTC), got.ExpiresAt.UTC())
	require.NotNil(t, got.RunnerID)
	assert.Equal(t, "run_0002xK9mNpV1StGXR8", *got.RunnerID)
	require.NotNil(t, got.CreatedBy)
	assert.Equal(t, "admin", *got.CreatedBy)
	assert.Equal(t, map[string]string{"env": "prod", "tier": "premium"}, got.Labels)

	assert.Equal(t, wantRawToken, got.RawToken)

	// The hash is not a field of the wire type at all any more, so there is
	// nothing to decode into and nothing for a caller to read back. That the
	// server withholds it is proved at the source, by
	// TestAdminResponsesWithholdSecrets in pkg/server/admin.
	assert.NotContains(t, string(loadFixture(t, "runner_token_create_response.json")), "hash")
}

func TestRunnerTokenWithSecret_UnmarshalJSON_CreateResponse(t *testing.T) {
	var got RunnerTokenWithSecret
	require.NoError(t, json.Unmarshal(loadFixture(t, "runner_token_create_response.json"), &got))
	assertDecodedToken(t, &got, "rtok_8Kd2mVp1SecretValueDoNotLog")
}

func TestRunnerTokenWithSecret_UnmarshalJSON_RotateResponse(t *testing.T) {
	var got RunnerTokenWithSecret
	require.NoError(t, json.Unmarshal(loadFixture(t, "runner_token_rotate_response.json"), &got))
	assertDecodedToken(t, &got, "rtok_9Ne3pWq2RotatedSecretValue")

	require.NotNil(t, got.RotationDeadline)
	assert.Equal(t, time.Date(2026, 8, 24, 11, 4, 5, 0, time.UTC), got.RotationDeadline.UTC())
}

// A response missing the token object must fail loudly. Decoding it into zero
// values is the original bug.
func TestRunnerTokenWithSecret_UnmarshalJSON_MissingToken(t *testing.T) {
	var got RunnerTokenWithSecret
	err := json.Unmarshal([]byte(`{"raw_token":"rtok_orphan"}`), &got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
}

func TestRunnerTokenWithSecret_RoundTrip(t *testing.T) {
	original := loadFixture(t, "runner_token_create_response.json")

	var decoded RunnerTokenWithSecret
	require.NoError(t, json.Unmarshal(original, &decoded))

	encoded, err := json.Marshal(decoded)
	require.NoError(t, err)
	assert.JSONEq(t, string(original), string(encoded),
		"the type must re-emit the shape it accepts")
}

// End-to-end through the client, so a future change to doRequest that bypasses
// the custom decoder is caught too.
func TestHTTPAdminClient_CreateRunnerToken_DecodesServerShape(t *testing.T) {
	fixture := loadFixture(t, "runner_token_create_response.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/admin/api/v1/runner-tokens", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	c := NewHTTPAdminClient(srv.URL, "admin", "secret")

	got, err := c.CreateRunnerToken(context.Background(), CreateRunnerTokenOptions{
		PoolName: "gpu-pool",
	})
	require.NoError(t, err)
	assertDecodedToken(t, got, "rtok_8Kd2mVp1SecretValueDoNotLog")
}

func TestHTTPAdminClient_RotateRunnerToken_DecodesServerShape(t *testing.T) {
	fixture := loadFixture(t, "runner_token_rotate_response.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/admin/api/v1/runner-tokens/rtok_0002xK9mNqW2TuHYS9/rotate", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	c := NewHTTPAdminClient(srv.URL, "admin", "secret")

	got, err := c.RotateRunnerToken(context.Background(), "rtok_0002xK9mNqW2TuHYS9")
	require.NoError(t, err)
	assertDecodedToken(t, got, "rtok_9Ne3pWq2RotatedSecretValue")
}

// The API key response has the same nested shape as the runner token one, and
// the SDK used to declare `Key string` against the {"key": {...}} object — so
// CreateAPIKey failed outright with "cannot unmarshal object into Go struct
// field ... of type string". It had never worked.
func TestAPIKeyWithSecret_UnmarshalJSON_CreateResponse(t *testing.T) {
	var got APIKeyWithSecret
	require.NoError(t, json.Unmarshal(loadFixture(t, "api_key_create_response.json"), &got))

	assert.Equal(t, "key_0002xK9mNrX3UvIZT0", got.ID, "ID must not be blank")
	assert.Equal(t, "ci", got.Name)
	assert.Equal(t, "mk_7Jc1lUo0", got.KeyPrefix, "prefix must not be blank")
	assert.Equal(t, []string{"sessions:*", "tasks:*"}, got.Scopes)
	assert.Equal(t, map[string]string{"env": "prod"}, got.Labels)
	assert.Equal(t, "mk_7Jc1lUo0SecretValueDoNotLog", got.RawToken)
	require.NotNil(t, got.CreatedBy)
	assert.Equal(t, "admin", *got.CreatedBy)

	assert.NotContains(t, string(loadFixture(t, "api_key_create_response.json")), "hash")
}

func TestAPIKeyWithSecret_RejectsAResponseWithNoKeyObject(t *testing.T) {
	var got APIKeyWithSecret
	err := json.Unmarshal([]byte(`{"raw_token":"mk_plain"}`), &got)
	require.Error(t, err, "decoding into zero values is the bug this guards")
	assert.Contains(t, err.Error(), "key")
}

func TestAPIKeyWithSecret_RoundTrip(t *testing.T) {
	original := loadFixture(t, "api_key_create_response.json")

	var got APIKeyWithSecret
	require.NoError(t, json.Unmarshal(original, &got))
	reEmitted, err := json.Marshal(got)
	require.NoError(t, err)

	var want, have map[string]any
	require.NoError(t, json.Unmarshal(original, &want))
	require.NoError(t, json.Unmarshal(reEmitted, &have))
	assert.Equal(t, want, have, "the type must re-emit the shape it accepts")
}
