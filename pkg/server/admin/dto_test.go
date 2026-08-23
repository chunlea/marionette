package admin

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
	"github.com/chunlea/marionette/pkg/store"
)

// secretFieldNames are the columns that must never appear on the admin wire,
// by their store-model JSON tag. Several already carry `json:"-"`; the point
// is that the mappers do not copy them at all, so removing a tag by accident
// cannot expose one.
var secretFieldNames = []string{
	"key_hash",
	"token_hash",
	"previous_token_hash",
	"hash_version",
	"api_key_encrypted",
	"secret_encrypted",
	"secret_hash",
	"dek_encrypted",
	"tenant_id",
}

// theSecret is planted in every secret-bearing field. Any response containing
// it has leaked, wherever the value travelled from.
const theSecret = "SECRET-MUST-NOT-APPEAR"

func marshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

func assertNoSecrets(t *testing.T, v any) {
	t.Helper()
	rendered := marshal(t, v)

	var keys map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(rendered), &keys))
	for _, field := range secretFieldNames {
		assert.NotContains(t, keys, field, "a secret field reached the admin wire")
	}
	assert.NotContains(t, rendered, theSecret, "a secret value reached the admin wire")
}

func TestAdminResponsesWithholdSecrets(t *testing.T) {
	now := time.Now().UTC()
	secret := theSecret
	blob := json.RawMessage(`{"note":"` + theSecret + `"}`)

	t.Run("api key", func(t *testing.T) {
		got := toAPIKeyResponse(&store.APIKey{
			ID: "key_1", Name: "ci", KeyPrefix: "mk_abc",
			KeyHash: theSecret, HashVersion: 2, TenantID: &secret,
			Scopes: []string{"*"}, CreatedAt: now,
		})
		assertNoSecrets(t, got)
		assert.Equal(t, "mk_abc", got.KeyPrefix, "the prefix is what identifies a key without disclosing it")
	})

	t.Run("runner token", func(t *testing.T) {
		got := toRunnerTokenResponse(&store.RunnerToken{
			ID: "rt_1", TokenPrefix: "rt_abc", PoolName: "default", Status: "active",
			TokenHash: theSecret, PreviousTokenHash: &secret, HashVersion: 2, TenantID: &secret,
			CreatedAt: now,
		})
		assertNoSecrets(t, got)
		assert.Equal(t, "rt_abc", got.TokenPrefix)
	})

	t.Run("agent config", func(t *testing.T) {
		got := toAgentConfigResponse(&store.AgentConfig{
			ID: "ac_1", Name: "claude-prod", Agent: "claude",
			APIKeyEncrypted: theSecret, TenantID: &secret,
			CreatedAt: now, UpdatedAt: now,
		})
		assertNoSecrets(t, got)
	})

	t.Run("webhook", func(t *testing.T) {
		got := toWebhookResponse(&store.Webhook{
			ID: "wh_1", Name: "ci", URL: "https://example.test/hook",
			SecretEncrypted: theSecret, SecretHash: theSecret, SecretPrefix: "whsec_ab",
			TenantID: &secret, CreatedAt: now, UpdatedAt: now,
		})
		assertNoSecrets(t, got)
		assert.Equal(t, "whsec_ab", got.SecretPrefix)
	})

	t.Run("provider config", func(t *testing.T) {
		got := toProviderConfigResponse(&store.ProviderConfig{
			ID: "pc_1", Name: "docker-local", Provider: "docker", TenantID: &secret,
			CreatedAt: now, UpdatedAt: now,
		})
		assertNoSecrets(t, got)
	})

	t.Run("profile", func(t *testing.T) {
		got := toProfileResponse(&store.Profile{
			ID: "prof_1", Name: "default", TenantID: &secret, CreatedAt: now, UpdatedAt: now,
		})
		assertNoSecrets(t, got)
	})

	t.Run("runner", func(t *testing.T) {
		got := toRunnerResponse(&store.Runner{
			ID: "run_1", Status: "idle", TenantID: &secret, CreatedAt: now, UpdatedAt: now,
		})
		assertNoSecrets(t, got)
	})

	t.Run("webhook event", func(t *testing.T) {
		got := toWebhookEventResponse(&store.WebhookEvent{
			ID: "whe_1", WebhookID: "wh_1", EventType: "task.completed",
			Status: store.WebhookEventStatusPending, TenantID: &secret,
			CreatedAt: now, UpdatedAt: now,
		})
		assertNoSecrets(t, got)
		assert.Equal(t, "pending", got.Status)
	})

	t.Run("action log", func(t *testing.T) {
		got := toActionLogResponse(&store.ActionLog{
			ID: "log_1", ActorType: "api_key", Action: "session.create",
			ResourceType: "session", ResourceID: "sess_1",
			TenantID: &secret, CreatedAt: now,
		})
		assertNoSecrets(t, got)
	})

	t.Run("a free-form blob is still passed through", func(t *testing.T) {
		// Provider config and audit details are operator-authored: whatever
		// they put in comes back out. This documents that the secret sweep
		// above is about columns the server owns, not about scrubbing values.
		got := toProviderConfigResponse(&store.ProviderConfig{ID: "pc_1", Config: blob})
		assert.Equal(t, theSecret, got.Config["note"])
	})
}

// The create and rotate responses are the one place a secret is readable, and
// scripts/smoke.sh bootstraps its credentials from the field name.
func TestCreatedCredentialsExposeTheSecretExactlyOnce(t *testing.T) {
	t.Run("api key", func(t *testing.T) {
		body := marshal(t, admintypes.CreatedAPIKey{
			Key:      toAPIKeyResponse(&store.APIKey{ID: "key_1", KeyPrefix: "mk_abc", KeyHash: theSecret}),
			RawToken: "mk_plain",
		})
		assert.Contains(t, body, `"raw_token":"mk_plain"`)
		assert.Contains(t, body, `"key":{`)
		assert.NotContains(t, body, "key_hash")
	})

	t.Run("runner token", func(t *testing.T) {
		body := marshal(t, admintypes.CreatedRunnerToken{
			Token:    toRunnerTokenResponse(&store.RunnerToken{ID: "rt_1", TokenPrefix: "rt_abc", TokenHash: theSecret}),
			RawToken: "rt_plain",
		})
		assert.Contains(t, body, `"raw_token":"rt_plain"`)
		assert.Contains(t, body, `"token":{`)
		assert.NotContains(t, body, "token_hash")
	})
}

func TestAdminListEnvelopeReportsHasMore(t *testing.T) {
	// The admin envelope used to omit has_more entirely, so no client could
	// tell a full page from the last one. A cursor to fetch the next page is
	// exactly the condition.
	withCursor := toListResponse(&ListResult[store.APIKey]{
		Items:      []*store.APIKey{{ID: "key_1"}},
		NextCursor: "cursor_2",
		TotalCount: 42,
	}, toAPIKeyResponse)
	assert.True(t, withCursor.HasMore)
	assert.Equal(t, int64(42), withCursor.TotalCount)
	assert.Equal(t, "cursor_2", withCursor.NextCursor)

	lastPage := toListResponse(&ListResult[store.APIKey]{
		Items: []*store.APIKey{{ID: "key_1"}},
	}, toAPIKeyResponse)
	assert.False(t, lastPage.HasMore)

	body := marshal(t, lastPage)
	assert.Contains(t, body, `"has_more":false`)
	assert.Contains(t, body, `"total_count":0`)
}

func TestAdminEmptyListsSerializeAsArrays(t *testing.T) {
	assert.JSONEq(t, `{"items":[],"total_count":0,"has_more":false}`,
		marshal(t, toListResponse[store.APIKey](nil, toAPIKeyResponse)))
	assert.JSONEq(t, `{"items":[],"total_count":0,"has_more":false}`,
		marshal(t, toStoreListResponse[store.Webhook](nil, toWebhookResponse)))
}

func TestAdminMapsAreNeverNull(t *testing.T) {
	got := toAPIKeyResponse(&store.APIKey{ID: "key_1"})
	require.NotNil(t, got.Labels)
	require.NotNil(t, got.Annotations)
	require.NotNil(t, got.Scopes)

	body := marshal(t, got)
	for _, nullField := range []string{`"labels":null`, `"annotations":null`, `"scopes":null`} {
		assert.NotContains(t, body, nullField)
	}
}

func TestAdminBlobDecodingSurvivesUnexpectedShapes(t *testing.T) {
	// A column holding an array where the contract says object, or invalid
	// JSON, must not fail a read path.
	profile := toProfileResponse(&store.Profile{
		ID:        "prof_1",
		Resources: json.RawMessage(`["not","an","object"]`),
		Tunnels:   json.RawMessage(`{"not":"a list"}`),
		Network:   json.RawMessage(`not json at all`),
	})
	assert.Empty(t, profile.Resources)
	assert.Empty(t, profile.Tunnels)
	assert.Empty(t, profile.Network)
	assert.NotContains(t, marshal(t, profile), "null")
}

func TestProfileTunnelsRoundTrip(t *testing.T) {
	profile := toProfileResponse(&store.Profile{
		ID:      "prof_1",
		Tunnels: json.RawMessage(`[{"type":"http","port":3000}]`),
	})
	require.Len(t, profile.Tunnels, 1)
	assert.Equal(t, "http", profile.Tunnels[0]["type"])
	assert.Contains(t, marshal(t, profile), `"port":3000`)
}

func TestSecretFieldNamesCoverEveryStoreSecret(t *testing.T) {
	// If a new secret column appears with `json:"-"`, this test is the nudge
	// to add it to the sweep above rather than trusting the tag.
	for _, model := range []any{
		store.APIKey{}, store.RunnerToken{}, store.AgentConfig{}, store.Webhook{},
	} {
		rendered := marshal(t, model)
		for _, field := range secretFieldNames {
			if strings.Contains(rendered, `"`+field+`"`) && field != "hash_version" && field != "tenant_id" {
				t.Errorf("store model %T serializes %s directly; the sweep assumes it cannot", model, field)
			}
		}
	}
}
