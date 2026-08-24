package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The admin runner endpoints are what `mctl admin runners` decodes. The
// fixtures served here are generated from the server's own response types
// (admintypes.Runner and its list envelope) by gen_admin_fixtures.go, so a
// change to the wire shape shows up as a fixture diff instead of leaving these
// tests asserting a shape the server no longer sends. Regenerate them with
// `go generate ./pkg/client/...`.

// adminFixtureServer answers every request with the named fixture.
func adminFixtureServer(t *testing.T, status int, fixture string) *HTTPAdminClient {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(loadFixture(t, fixture))
	}))
	t.Cleanup(server.Close)

	return NewHTTPAdminClient(server.URL, "admin", "secret")
}

// assertSpawnedRunner checks the fields `mctl admin runners` prints.
func assertSpawnedRunner(t *testing.T, got *AdminRunner) {
	t.Helper()

	assert.Equal(t, "run_0002xK9mNpV1StGXR8", got.ID, "ID must not be blank")
	assert.Equal(t, "docker-runner-1", got.Name)
	assert.Equal(t, "runner-1.local", got.Hostname)
	assert.Equal(t, "idle", got.Status)
	assert.False(t, got.Tainted)
	assert.Nil(t, got.TaintReason)
	assert.Equal(t, "runner-is-sandbox", got.SandboxMode)
	assert.Equal(t, []string{"docker"}, got.SandboxTypes)
	assert.Equal(t, []string{"desktop", "android"}, got.Capabilities)
	assert.Equal(t, map[string]string{"env": "prod"}, got.Labels)

	// The operator's view exists for these three: the public runner has none
	// of them, so a blank here is the whole command losing its point.
	require.NotNil(t, got.ProviderConfigID)
	assert.Equal(t, "pcfg_0002xK9mNsY4VwJaU1", *got.ProviderConfigID)
	require.NotNil(t, got.ProviderInstanceID)
	assert.Equal(t, "b3f1c9d2e4a5", *got.ProviderInstanceID)
	require.NotNil(t, got.ProfileID)
	assert.Equal(t, "prof_0002xK9mNtZ5WxKbV2", *got.ProfileID)

	require.NotNil(t, got.PoolName)
	assert.Equal(t, "gpu-pool", *got.PoolName)
	require.NotNil(t, got.LastSeenAt)
	assert.Equal(t, time.Date(2026, 8, 23, 11, 4, 35, 0, time.UTC), got.LastSeenAt.UTC())
	assert.Equal(t, time.Date(2026, 8, 23, 11, 4, 5, 0, time.UTC), got.CreatedAt.UTC())
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestHTTPAdminClient_SpawnRunner_DecodesResponse(t *testing.T) {
	client := adminFixtureServer(t, http.StatusCreated, "runner_response.json")

	got, err := client.SpawnRunner(context.Background(), SpawnRunnerOptions{
		Name:             "docker-runner-1",
		ProviderConfigID: "pcfg_0002xK9mNsY4VwJaU1",
	})
	require.NoError(t, err)
	require.NotNil(t, got)

	assertSpawnedRunner(t, got)
}

func TestHTTPAdminClient_GetRunner_DecodesResponse(t *testing.T) {
	client := adminFixtureServer(t, http.StatusOK, "runner_response.json")

	got, err := client.GetRunner(context.Background(), "run_0002xK9mNpV1StGXR8")
	require.NoError(t, err)
	require.NotNil(t, got)

	assertSpawnedRunner(t, got)
}

func TestHTTPAdminClient_GetRunner_RequestsTheRunnerPath(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "runner_response.json"))
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	_, err := client.GetRunner(context.Background(), "run_test123")
	require.NoError(t, err)

	assert.Equal(t, "/admin/api/v1/runners/run_test123", path)
}

func TestHTTPAdminClient_ListRunners_DecodesResponse(t *testing.T) {
	client := adminFixtureServer(t, http.StatusOK, "runner_list_response.json")

	result, err := client.ListRunners(context.Background(), ListRunnersOptions{})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Items, 2)

	assertSpawnedRunner(t, result.Items[0])

	// The second row has none of the optional fields set, which is the shape
	// a pool runner that never reported in arrives as.
	second := result.Items[1]
	assert.Equal(t, "run_0002xK9mNuA6XyLcW3", second.ID)
	assert.Equal(t, "offline", second.Status)
	assert.True(t, second.Tainted)
	require.NotNil(t, second.TaintReason)
	assert.Equal(t, "disk pressure", *second.TaintReason)
	assert.Nil(t, second.ProviderConfigID)
	assert.Nil(t, second.PoolName)
	assert.Nil(t, second.LastSeenAt)

	assert.Equal(t, int64(2), result.TotalCount)
	assert.True(t, result.HasMore)
	assert.Equal(t, "cursor_0002xK9mNvB7", result.NextCursor)
}

// The admin API reads one comma-separated status parameter and one "labels"
// parameter of key=value pairs. The public API's repeated status and
// labels[key]=value are a different contract, and sending those to the admin
// endpoints dropped the filter silently.
func TestHTTPAdminClient_ListRunners_EncodesAdminFilters(t *testing.T) {
	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "runner_list_response.json"))
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	_, err := client.ListRunners(context.Background(), ListRunnersOptions{
		Limit:    10,
		Cursor:   "cursor_abc",
		Status:   []string{"idle", "busy"},
		PoolName: "gpu-pool",
		Labels:   map[string]string{"team": "backend", "env": "prod"},
	})
	require.NoError(t, err)

	assert.Equal(t, "10", query.Get("limit"))
	assert.Equal(t, "cursor_abc", query.Get("cursor"))
	assert.Equal(t, "idle,busy", query.Get("status"))
	assert.Equal(t, "gpu-pool", query.Get("pool_name"))
	assert.Equal(t, "env=prod,team=backend", query.Get("labels"), "pairs are sorted, so the query is stable")
	assert.Empty(t, query.Get("labels[env]"), "the public API's label format is not the admin API's")
}

// Every admin list endpoint shares the label encoding, so every one of them
// was dropping the filter. Cover them all, not just the new one.
func TestHTTPAdminClient_ListEndpointsEncodeLabelsForTheAdminAPI(t *testing.T) {
	labels := map[string]string{"env": "prod"}

	calls := map[string]func(c *HTTPAdminClient) error{
		"keys": func(c *HTTPAdminClient) error {
			_, err := c.ListAPIKeys(context.Background(), ListAPIKeysOptions{Labels: labels})
			return err
		},
		"agent-configs": func(c *HTTPAdminClient) error {
			_, err := c.ListAgentConfigs(context.Background(), ListAgentConfigsOptions{Labels: labels})
			return err
		},
		"provider-configs": func(c *HTTPAdminClient) error {
			_, err := c.ListProviderConfigs(context.Background(), ListProviderConfigsOptions{Labels: labels})
			return err
		},
		"runner-tokens": func(c *HTTPAdminClient) error {
			_, err := c.ListRunnerTokens(context.Background(), ListRunnerTokensOptions{Labels: labels})
			return err
		},
		"profiles": func(c *HTTPAdminClient) error {
			_, err := c.ListProfiles(context.Background(), ListProfilesOptions{Labels: labels})
			return err
		},
		"runners": func(c *HTTPAdminClient) error {
			_, err := c.ListRunners(context.Background(), ListRunnersOptions{Labels: labels})
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			var query url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query = r.URL.Query()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"items":[],"total_count":0,"has_more":false}`))
			}))
			defer server.Close()

			require.NoError(t, call(NewHTTPAdminClient(server.URL, "admin", "secret")))
			assert.Equal(t, "env=prod", query.Get("labels"))
		})
	}
}

func TestHTTPAdminClient_ListRunners_PropagatesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"code":"not_implemented","message":"runner service not configured"}`))
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	_, err := client.ListRunners(context.Background(), ListRunnersOptions{})

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotImplemented, apiErr.StatusCode)
	assert.Equal(t, "not_implemented", apiErr.Code)
}
