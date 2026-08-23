package admin

import (
	"encoding/json"

	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
	"github.com/chunlea/marionette/pkg/store"
)

// This file is the only place an internal model becomes admin JSON.
//
// The admin API is the surface that holds credentials, so the mappers name
// every field they copy rather than relying on `json:"-"`: a secret that is
// never assigned cannot be exposed by someone adding a tag in the wrong place,
// and TestAdminResponsesWithholdSecrets holds the line.

// decodeStringMap converts a jsonb column into a string map. The result is
// never nil: labels and headers are non-nullable in the contract, so a NULL
// column and an absent key both render as {}.
func decodeStringMap(raw json.RawMessage) map[string]string {
	out := map[string]string{}
	if len(raw) == 0 {
		return out
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return out
	}
	for k, v := range decoded {
		out[k] = v
	}
	return out
}

// decodeObject converts a free-form jsonb column into a map. Provider configs,
// profile resources and audit details are all caller-shaped, so the values
// stay `any`.
func decodeObject(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return out
	}
	for k, v := range decoded {
		out[k] = v
	}
	return out
}

// decodeObjectList converts a jsonb array of objects, e.g. a profile's tunnels.
func decodeObjectList(raw json.RawMessage) []map[string]any {
	out := []map[string]any{}
	if len(raw) == 0 {
		return out
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return out
	}
	out = append(out, decoded...)
	return out
}

// nonNilStrings returns a non-nil slice so JSON arrays never render as null.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// toAPIKeyResponse maps a stored API key onto the wire contract.
// The key hash and its version are absent by construction.
func toAPIKeyResponse(k *store.APIKey) *admintypes.APIKey {
	if k == nil {
		return nil
	}
	return &admintypes.APIKey{
		ID:           k.ID,
		Name:         k.Name,
		KeyPrefix:    k.KeyPrefix,
		Scopes:       nonNilStrings(k.Scopes),
		Labels:       decodeStringMap(k.Labels),
		Annotations:  decodeStringMap(k.Annotations),
		CreatedAt:    k.CreatedAt,
		CreatedBy:    k.CreatedBy,
		LastUsedAt:   k.LastUsedAt,
		ExpiresAt:    k.ExpiresAt,
		RevokedAt:    k.RevokedAt,
		RevokeReason: k.RevokeReason,
	}
}

// toRunnerTokenResponse maps a stored runner token onto the wire contract.
// Neither the current hash nor the previous one is copied.
func toRunnerTokenResponse(t *store.RunnerToken) *admintypes.RunnerToken {
	if t == nil {
		return nil
	}
	return &admintypes.RunnerToken{
		ID:               t.ID,
		TokenPrefix:      t.TokenPrefix,
		RunnerID:         t.RunnerID,
		PoolName:         t.PoolName,
		Status:           t.Status,
		RotationDeadline: t.RotationDeadline,
		Labels:           decodeStringMap(t.Labels),
		CreatedAt:        t.CreatedAt,
		CreatedBy:        t.CreatedBy,
		LastUsedAt:       t.LastUsedAt,
		ExpiresAt:        t.ExpiresAt,
		RevokedAt:        t.RevokedAt,
		RevokeReason:     t.RevokeReason,
	}
}

// toAgentConfigResponse maps a stored agent config onto the wire contract.
// The encrypted agent API key is write-only and is not copied.
func toAgentConfigResponse(c *store.AgentConfig) *admintypes.AgentConfig {
	if c == nil {
		return nil
	}
	return &admintypes.AgentConfig{
		ID:          c.ID,
		Name:        c.Name,
		Agent:       c.Agent,
		Model:       c.Model,
		BaseURL:     c.BaseURL,
		Extra:       decodeObject(c.Extra),
		IsDefault:   c.IsDefault,
		Labels:      decodeStringMap(c.Labels),
		Annotations: decodeStringMap(c.Annotations),
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// toProviderConfigResponse maps a stored provider config onto the wire contract.
func toProviderConfigResponse(c *store.ProviderConfig) *admintypes.ProviderConfig {
	if c == nil {
		return nil
	}
	return &admintypes.ProviderConfig{
		ID:            c.ID,
		Name:          c.Name,
		Provider:      c.Provider,
		Config:        decodeObject(c.Config),
		SuspendConfig: decodeObject(c.SuspendConfig),
		IsDefault:     c.IsDefault,
		Labels:        decodeStringMap(c.Labels),
		Annotations:   decodeStringMap(c.Annotations),
		CreatedAt:     c.CreatedAt,
		UpdatedAt:     c.UpdatedAt,
	}
}

// toProfileResponse maps a stored profile onto the wire contract.
func toProfileResponse(p *store.Profile) *admintypes.Profile {
	if p == nil {
		return nil
	}
	return &admintypes.Profile{
		ID:               p.ID,
		Name:             p.Name,
		Description:      p.Description,
		ProviderConfigID: p.ProviderConfigID,
		Resources:        decodeObject(p.Resources),
		Network:          decodeObject(p.Network),
		InitScript:       p.InitScript,
		CleanupScript:    p.CleanupScript,
		Tunnels:          decodeObjectList(p.Tunnels),
		Selector:         decodeObject(p.Selector),
		IsBuiltin:        p.IsBuiltin,
		Labels:           decodeStringMap(p.Labels),
		Annotations:      decodeStringMap(p.Annotations),
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
	}
}

// toRunnerResponse maps a stored runner onto the operator's view, which unlike
// the public one names the provider backing the runner.
func toRunnerResponse(r *store.Runner) *admintypes.Runner {
	if r == nil {
		return nil
	}
	return &admintypes.Runner{
		ID:                 r.ID,
		Name:               r.Name,
		Hostname:           r.Hostname,
		Status:             r.Status,
		Tainted:            r.Tainted,
		TaintReason:        r.TaintReason,
		SandboxMode:        r.SandboxMode,
		SandboxTypes:       nonNilStrings(r.SandboxTypes),
		ProviderConfigID:   r.ProviderConfigID,
		ProviderInstanceID: r.ProviderInstanceID,
		PoolName:           r.PoolName,
		ProfileID:          r.ProfileID,
		Capabilities:       nonNilStrings(r.Capabilities),
		Labels:             decodeStringMap(r.Labels),
		Annotations:        decodeStringMap(r.Annotations),
		LastSeenAt:         r.LastSeenAt,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

// toWebhookResponse maps a stored webhook onto the wire contract. Neither the
// encrypted signing secret nor its hash is copied.
func toWebhookResponse(w *store.Webhook) *admintypes.Webhook {
	if w == nil {
		return nil
	}
	return &admintypes.Webhook{
		ID:                w.ID,
		Name:              w.Name,
		URL:               w.URL,
		Events:            nonNilStrings(w.Events),
		SecretPrefix:      w.SecretPrefix,
		IsActive:          w.IsActive,
		MaxRetries:        w.MaxRetries,
		RetryDelaySeconds: w.RetryDelaySeconds,
		TimeoutSeconds:    w.TimeoutSeconds,
		Headers:           decodeStringMap(w.Headers),
		Labels:            decodeStringMap(w.Labels),
		Annotations:       decodeStringMap(w.Annotations),
		CreatedAt:         w.CreatedAt,
		UpdatedAt:         w.UpdatedAt,
	}
}

// toWebhookEventResponse maps a stored delivery attempt onto the wire contract.
func toWebhookEventResponse(e *store.WebhookEvent) *admintypes.WebhookEvent {
	if e == nil {
		return nil
	}
	return &admintypes.WebhookEvent{
		ID:             e.ID,
		WebhookID:      e.WebhookID,
		EventType:      e.EventType,
		Payload:        decodeObject(e.Payload),
		Status:         string(e.Status),
		Attempts:       e.Attempts,
		LastError:      e.LastError,
		LastStatusCode: e.LastStatusCode,
		NextRetryAt:    e.NextRetryAt,
		DeliveredAt:    e.DeliveredAt,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
	}
}

// toActionLogResponse maps a stored audit entry onto the wire contract.
func toActionLogResponse(l *store.ActionLog) *admintypes.ActionLog {
	if l == nil {
		return nil
	}
	return &admintypes.ActionLog{
		ID:           l.ID,
		ActorType:    l.ActorType,
		ActorID:      l.ActorID,
		ActorName:    l.ActorName,
		Action:       l.Action,
		ResourceType: l.ResourceType,
		ResourceID:   l.ResourceID,
		SessionID:    l.SessionID,
		TaskID:       l.TaskID,
		Details:      decodeObject(l.Details),
		IPAddress:    l.IPAddress,
		UserAgent:    l.UserAgent,
		Success:      l.Success,
		ErrorMessage: l.ErrorMessage,
		CreatedAt:    l.CreatedAt,
	}
}

// toListResponse maps an admin list result onto the shared envelope.
func toListResponse[S any, D any](res *ListResult[S], convert func(*S) *D) *admintypes.ListResponse[D] {
	out := &admintypes.ListResponse[D]{Items: []*D{}}
	if res == nil {
		return out
	}
	out.TotalCount = res.TotalCount
	out.NextCursor = res.NextCursor
	// The admin service layer never reported has_more; a further page exists
	// exactly when it handed back a cursor to fetch it with.
	out.HasMore = res.NextCursor != ""
	for _, item := range res.Items {
		out.Items = append(out.Items, convert(item))
	}
	return out
}

// toStoreListResponse maps a store list result onto the shared envelope, for
// the webhook service, which returns the store's own result type.
func toStoreListResponse[S any, D any](res *store.ListResult[S], convert func(*S) *D) *admintypes.ListResponse[D] {
	out := &admintypes.ListResponse[D]{Items: []*D{}}
	if res == nil {
		return out
	}
	out.TotalCount = res.TotalCount
	out.HasMore = res.HasMore
	out.NextCursor = res.NextCursor
	for _, item := range res.Items {
		out.Items = append(out.Items, convert(item))
	}
	return out
}
