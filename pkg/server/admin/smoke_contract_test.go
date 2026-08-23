package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scripts/smoke.sh bootstraps every credential it needs from two responses,
// reading one field out of each with python:
//
//	POST /admin/api/v1/keys          -> ["raw_token"]
//	POST /admin/api/v1/runner-tokens -> ["raw_token"]
//
// The smoke script is coordinator-owned and lanes do not edit it, so those two
// field names are load-bearing in a way no compiler enforces. These tests go
// through the real router and assert on raw JSON, not on a decoded struct: a
// renamed field would still decode into a Go type that had been renamed with
// it, and only the untyped view catches that.

func decodeRawJSON(t *testing.T, body *bytes.Buffer) map[string]json.RawMessage {
	t.Helper()
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body.Bytes(), &got))
	return got
}

func TestSmokeBootstrapFieldsSurviveOnKeyCreate(t *testing.T) {
	srv := newTestServer(WithAPIKeyService(NewMockAPIKeyService()))

	body, err := json.Marshal(CreateAPIKeyRequest{Name: "smoke", Scopes: []string{"*"}})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/keys/", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	fields := decodeRawJSON(t, rr.Body)
	require.Contains(t, fields, "raw_token", "scripts/smoke.sh reads this field to bootstrap its API key")
	require.Contains(t, fields, "key", "the key object sits alongside the raw token")

	var rawToken string
	require.NoError(t, json.Unmarshal(fields["raw_token"], &rawToken))
	assert.NotEmpty(t, rawToken)

	// The credential is readable here and nowhere else in the response.
	keyFields := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(fields["key"], &keyFields))
	assert.NotContains(t, keyFields, "key_hash")
	assert.NotContains(t, keyFields, "raw_token")
	assert.Contains(t, keyFields, "key_prefix")
}

func TestSmokeBootstrapFieldsSurviveOnRunnerTokenCreate(t *testing.T) {
	srv := newTestServer(WithRunnerTokenAdminService(NewMockRunnerTokenService()))

	body, err := json.Marshal(CreateRunnerTokenRequest{PoolName: "default"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens/", bytes.NewReader(body))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	fields := decodeRawJSON(t, rr.Body)
	require.Contains(t, fields, "raw_token", "scripts/smoke.sh reads this field to bootstrap its runner token")
	require.Contains(t, fields, "token")

	tokenFields := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(fields["token"], &tokenFields))
	assert.NotContains(t, tokenFields, "token_hash")
	assert.NotContains(t, tokenFields, "previous_token_hash")
	assert.Contains(t, tokenFields, "token_prefix")
	assert.Contains(t, tokenFields, "pool_name")
}

// The smoke walk asserts an unauthenticated admin call is refused before it
// bootstraps anything; that fail-closed behaviour is A2's, and this pins that
// the contract work did not loosen it.
func TestSmokeAdminStillFailsClosed(t *testing.T) {
	srv := newTestServer(WithAPIKeyService(NewMockAPIKeyService()))

	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys/", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Header().Get("WWW-Authenticate"), "Basic")
}
