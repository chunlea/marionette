package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/tunnel"
)

// internalFields are database columns that must never reach a client. They are
// listed by their store-model JSON tag: if someone deletes a mapper field or
// serializes a store model directly again, these reappear and this test fails.
var internalFields = []string{
	"tenant_id",
	"context_snapshot",
	"agent_config_metadata",
	"suspend_snapshot_id",
	"suspend_workspace_synced",
	"storage_config",
	"storage_domain",
	"storage_key",
	"token_hash",
	"token_prefix",
	"hash_version",
	"provider_config_id",
	"provider_instance_id",
}

// marshalKeys returns the top-level JSON keys of v.
func marshalKeys(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	var keys map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &keys))
	return keys
}

func assertNoInternalFields(t *testing.T, v any) {
	t.Helper()
	keys := marshalKeys(t, v)
	for _, field := range internalFields {
		assert.NotContains(t, keys, field, "internal field leaked onto the wire")
	}
}

func TestResponsesOmitInternalFields(t *testing.T) {
	now := time.Now().UTC()
	str := "x"
	num := 1
	raw := json.RawMessage(`{"secret":"value"}`)

	t.Run("session", func(t *testing.T) {
		got := toSessionResponse(&store.Session{
			ID: "sess_1", Status: "active", Agent: "claude", WorkspaceID: "ws_1",
			TenantID:               &str,
			ContextSnapshot:        raw,
			AgentConfigMetadata:    raw,
			SuspendSnapshotID:      &str,
			SuspendWorkspaceSynced: new(bool),
			CreatedAt:              now, UpdatedAt: now,
		})
		assertNoInternalFields(t, got)
		assert.Equal(t, "sess_1", got.ID)
		assert.Equal(t, "active", got.Status)
	})

	t.Run("task", func(t *testing.T) {
		got := toTaskResponse(&store.Task{ID: "task_1", Status: "pending", TenantID: &str})
		assertNoInternalFields(t, got)
		assert.Equal(t, "task_1", got.ID)
	})

	t.Run("task run", func(t *testing.T) {
		got := toTaskRunResponse(&store.TaskRun{ID: "run_1", TaskID: "task_1", Status: "running", TenantID: &str})
		assertNoInternalFields(t, got)
		assert.Equal(t, "run_1", got.ID)
	})

	t.Run("runner", func(t *testing.T) {
		got := toRunnerResponse(&store.Runner{
			ID: "run_1", Status: "idle", TenantID: &str,
			ProviderConfigID: &str, ProviderInstanceID: &str,
		})
		assertNoInternalFields(t, got)
	})

	t.Run("permission", func(t *testing.T) {
		got := toPermissionResponse(&store.PermissionRequest{ID: "perm_1", Status: "pending", TenantID: &str})
		assertNoInternalFields(t, got)
		assert.Equal(t, "perm_1", got.ID)
	})

	t.Run("workspace", func(t *testing.T) {
		got := toWorkspaceResponse(&store.Workspace{
			ID: "ws_1", TenantID: &str, StorageConfig: raw,
			StorageDomain: &str, StorageKey: &str, DiskQuotaMB: &num,
		})
		assertNoInternalFields(t, got)
		assert.Equal(t, &num, got.DiskQuotaMB)
	})

	t.Run("scheduled task", func(t *testing.T) {
		got := toScheduledTaskResponse(&store.ScheduledTask{ID: "sched_1", Status: "active", TenantID: &str})
		assertNoInternalFields(t, got)
	})

	t.Run("log", func(t *testing.T) {
		got := toLogResponse(&store.Log{ID: "log_1", Content: "hello", TenantID: &str})
		assertNoInternalFields(t, got)
		assert.Equal(t, "hello", got.Content)
	})
}

func TestTunnelResponseFoldsEmptyStringsToNull(t *testing.T) {
	full := toTunnelResponse(&tunnel.Tunnel{
		ID: "tun_1", SessionID: "sess_1", RunnerID: "runner_1",
		Type: "http", Direction: "outbound", LocalPort: 3000,
		PublicURL: "https://example.test/t", Token: "secret",
	})
	require.NotNil(t, full.RunnerID)
	assert.Equal(t, "runner_1", *full.RunnerID)
	require.NotNil(t, full.PublicURL)
	assert.Equal(t, "secret", full.Token, "the create response is the only place the token is readable")

	empty := toTunnelResponse(&tunnel.Tunnel{ID: "tun_2", SessionID: "sess_1", Type: "http"})
	assert.Nil(t, empty.RunnerID)
	assert.Nil(t, empty.PublicURL)

	keys := marshalKeys(t, empty)
	assert.NotContains(t, keys, "runner_id")
	assert.NotContains(t, keys, "public_url")
	assert.NotContains(t, keys, "token", "an unset token must not appear at all")
}

func TestLabelsAndAnnotationsAreNeverNull(t *testing.T) {
	// A NULL jsonb column and a malformed one both have to render as {}: the
	// TypeScript contract declares them non-nullable.
	for name, session := range map[string]*store.Session{
		"null column":      {ID: "sess_1"},
		"malformed column": {ID: "sess_1", Labels: json.RawMessage(`["not","a","map"]`)},
		"populated":        {ID: "sess_1", Labels: json.RawMessage(`{"env":"prod"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			got := toSessionResponse(session)
			assert.NotNil(t, got.Labels)
			assert.NotNil(t, got.Annotations)

			data, err := json.Marshal(got)
			require.NoError(t, err)
			assert.NotContains(t, string(data), `"labels":null`)
			assert.NotContains(t, string(data), `"annotations":null`)
		})
	}

	populated := toSessionResponse(&store.Session{Labels: json.RawMessage(`{"env":"prod"}`)})
	assert.Equal(t, map[string]string{"env": "prod"}, populated.Labels)
}

func TestListResponseCarriesPaginationMetadata(t *testing.T) {
	got := toListResponse(&store.ListResult[store.Task]{
		Items:      []*store.Task{{ID: "task_1"}, {ID: "task_2"}},
		TotalCount: 42,
		HasMore:    true,
		NextCursor: "cursor_2",
	}, toTaskResponse)

	assert.Equal(t, int64(42), got.TotalCount)
	assert.True(t, got.HasMore)
	assert.Equal(t, "cursor_2", got.NextCursor)
	require.Len(t, got.Items, 2)
	assert.Equal(t, "task_1", got.Items[0].ID)

	keys := marshalKeys(t, got)
	assert.Contains(t, keys, "total_count")
	assert.Contains(t, keys, "has_more")
}

func TestEmptyListsSerializeAsArrays(t *testing.T) {
	fromResult, err := json.Marshal(toListResponse(&store.ListResult[store.Task]{}, toTaskResponse))
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[],"total_count":0,"has_more":false}`, string(fromResult))

	fromSlice, err := json.Marshal(toSliceResponse(nil, toTunnelResponse))
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[],"total_count":0,"has_more":false}`, string(fromSlice))

	fromNil, err := json.Marshal(toListResponse[store.Task](nil, toTaskResponse))
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[],"total_count":0,"has_more":false}`, string(fromNil))
}

func TestSliceResponseCountsItems(t *testing.T) {
	got := toSliceResponse([]*tunnel.Tunnel{{ID: "tun_1"}, {ID: "tun_2"}}, toTunnelResponse)
	assert.Equal(t, int64(2), got.TotalCount)
	assert.False(t, got.HasMore)
	require.Len(t, got.Items, 2)
}
