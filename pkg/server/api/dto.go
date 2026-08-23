package api

import (
	"encoding/json"

	"github.com/chunlea/marionette/pkg/server/api/apitypes"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/tunnel"
)

// This file is the only place where an internal model becomes public JSON.
//
// Handlers must never serialize a store model directly: doing so turns every
// new database column into a public API field, which is how tenant_id,
// context_snapshot and the suspend bookkeeping ended up on the wire. Each
// mapper below names every field it copies, so adding a column is a no-op for
// the API until someone deliberately adds it here.

// decodeStringMap converts a jsonb column into a string map.
//
// The result is never nil: labels and annotations are declared non-nullable in
// the contract, so a NULL column and an absent key both render as {}. A value
// that is not a flat string map is dropped rather than failing the request —
// the alternative is a 500 on a read path over data the API cannot represent.
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

// emptyToNil maps an empty string to a nil pointer, so an unset value is
// omitted from JSON instead of appearing as "".
func emptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nonNilStrings returns a non-nil slice so JSON arrays never render as null.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// toSessionResponse maps a stored session onto the wire contract.
func toSessionResponse(s *store.Session) *apitypes.Session {
	if s == nil {
		return nil
	}
	return &apitypes.Session{
		ID:                 s.ID,
		Name:               s.Name,
		Status:             s.Status,
		Agent:              s.Agent,
		AgentVersion:       s.AgentVersion,
		AgentConfigID:      s.AgentConfigID,
		IsBYOK:             s.IsBYOK,
		RunnerID:           s.RunnerID,
		PreviousRunnerID:   s.PreviousRunnerID,
		WorkspaceID:        s.WorkspaceID,
		ProfileID:          s.ProfileID,
		NetworkPolicy:      s.NetworkPolicy,
		AllowedHosts:       nonNilStrings(s.AllowedHosts),
		LifecycleMode:      s.LifecycleMode,
		IdleTimeoutSeconds: s.IdleTimeoutSeconds,
		MaxLifetimeSeconds: s.MaxLifetimeSeconds,
		SuspendStrategy:    s.SuspendStrategy,
		ScheduleCron:       s.ScheduleCron,
		ScheduleTimezone:   s.ScheduleTimezone,
		NextScheduledAt:    s.NextScheduledAt,
		Labels:             decodeStringMap(s.Labels),
		Annotations:        decodeStringMap(s.Annotations),
		LastActivityAt:     s.LastActivityAt,
		SuspendedAt:        s.SuspendedAt,
		ResumedAt:          s.ResumedAt,
		CreatedAt:          s.CreatedAt,
		UpdatedAt:          s.UpdatedAt,
	}
}

// toTaskResponse maps a stored task onto the wire contract.
func toTaskResponse(t *store.Task) *apitypes.Task {
	if t == nil {
		return nil
	}
	return &apitypes.Task{
		ID:             t.ID,
		SessionID:      t.SessionID,
		Prompt:         t.Prompt,
		Status:         t.Status,
		MaxRetries:     t.MaxRetries,
		RetryCount:     t.RetryCount,
		TimeoutSeconds: t.TimeoutSeconds,
		Labels:         decodeStringMap(t.Labels),
		Annotations:    decodeStringMap(t.Annotations),
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

// toTaskRunResponse maps a stored task run onto the wire contract.
func toTaskRunResponse(r *store.TaskRun) *apitypes.TaskRun {
	if r == nil {
		return nil
	}
	return &apitypes.TaskRun{
		ID:           r.ID,
		TaskID:       r.TaskID,
		Attempt:      r.Attempt,
		RunnerID:     r.RunnerID,
		Status:       r.Status,
		Error:        r.Error,
		ExitCode:     r.ExitCode,
		TokensInput:  r.TokensInput,
		TokensOutput: r.TokensOutput,
		QueuedAt:     r.QueuedAt,
		AssignedAt:   r.AssignedAt,
		StartedAt:    r.StartedAt,
		EndedAt:      r.EndedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

// toRunnerResponse maps a stored runner onto the wire contract.
func toRunnerResponse(r *store.Runner) *apitypes.Runner {
	if r == nil {
		return nil
	}
	return &apitypes.Runner{
		ID:           r.ID,
		Name:         r.Name,
		Hostname:     r.Hostname,
		Status:       r.Status,
		Tainted:      r.Tainted,
		TaintReason:  r.TaintReason,
		SandboxMode:  r.SandboxMode,
		SandboxTypes: nonNilStrings(r.SandboxTypes),
		PoolName:     r.PoolName,
		ProfileID:    r.ProfileID,
		Capabilities: nonNilStrings(r.Capabilities),
		Labels:       decodeStringMap(r.Labels),
		Annotations:  decodeStringMap(r.Annotations),
		LastSeenAt:   r.LastSeenAt,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

// toPermissionResponse maps a stored permission request onto the wire contract.
func toPermissionResponse(p *store.PermissionRequest) *apitypes.PermissionRequest {
	if p == nil {
		return nil
	}
	return &apitypes.PermissionRequest{
		ID:                  p.ID,
		OriginalRequestID:   p.OriginalRequestID,
		SessionID:           p.SessionID,
		TaskID:              p.TaskID,
		RunID:               p.RunID,
		Tool:                p.Tool,
		Action:              p.Action,
		Context:             p.Context,
		RiskLevel:           p.RiskLevel,
		Status:              p.Status,
		SuspendAfterSeconds: p.SuspendAfterSeconds,
		RespondedBy:         p.RespondedBy,
		ResponseReason:      p.ResponseReason,
		RespondedAt:         p.RespondedAt,
		CreatedAt:           p.CreatedAt,
		UpdatedAt:           p.UpdatedAt,
	}
}

// toWorkspaceResponse maps a stored workspace onto the wire contract.
func toWorkspaceResponse(w *store.Workspace) *apitypes.Workspace {
	if w == nil {
		return nil
	}
	return &apitypes.Workspace{
		ID:               w.ID,
		Name:             w.Name,
		Persist:          w.Persist,
		StorageType:      w.StorageType,
		Mobility:         w.Mobility,
		StorageSizeBytes: w.StorageSizeBytes,
		DiskQuotaMB:      w.DiskQuotaMB,
		LastSyncedAt:     w.LastSyncedAt,
		Labels:           decodeStringMap(w.Labels),
		Annotations:      decodeStringMap(w.Annotations),
		ExpiresAt:        w.ExpiresAt,
		CreatedAt:        w.CreatedAt,
		UpdatedAt:        w.UpdatedAt,
		DeletedAt:        w.DeletedAt,
	}
}

// toScheduledTaskResponse maps a stored scheduled task onto the wire contract.
func toScheduledTaskResponse(s *store.ScheduledTask) *apitypes.ScheduledTask {
	if s == nil {
		return nil
	}
	return &apitypes.ScheduledTask{
		ID:                     s.ID,
		SessionID:              s.SessionID,
		Name:                   s.Name,
		Description:            s.Description,
		CronExpression:         s.CronExpression,
		Timezone:               s.Timezone,
		PromptTemplate:         s.PromptTemplate,
		TimeoutSeconds:         s.TimeoutSeconds,
		MaxRetries:             s.MaxRetries,
		Status:                 s.Status,
		NextRunAt:              s.NextRunAt,
		LastRunAt:              s.LastRunAt,
		LastTaskID:             s.LastTaskID,
		RunCount:               s.RunCount,
		OnFailure:              s.OnFailure,
		FailureCount:           s.FailureCount,
		ConsecutiveFailures:    s.ConsecutiveFailures,
		MaxConsecutiveFailures: s.MaxConsecutiveFailures,
		Labels:                 decodeStringMap(s.Labels),
		Annotations:            decodeStringMap(s.Annotations),
		CreatedAt:              s.CreatedAt,
		UpdatedAt:              s.UpdatedAt,
	}
}

// toTunnelResponse maps a live tunnel onto the wire contract.
//
// tunnel.Tunnel uses empty strings where the database uses NULL; the contract
// uses omitted fields, so the empty values are folded back into nil here.
func toTunnelResponse(t *tunnel.Tunnel) *apitypes.Tunnel {
	if t == nil {
		return nil
	}
	return &apitypes.Tunnel{
		ID:        t.ID,
		SessionID: t.SessionID,
		RunnerID:  emptyToNil(t.RunnerID),
		Type:      t.Type,
		Direction: t.Direction,
		LocalPort: t.LocalPort,
		PublicURL: emptyToNil(t.PublicURL),
		IsPublic:  t.IsPublic,
		Token:     t.Token,
		ExpiresAt: t.ExpiresAt,
		CreatedAt: t.CreatedAt,
		ClosedAt:  t.ClosedAt,
	}
}

// toLogResponse maps a stored log line onto the wire contract.
func toLogResponse(l *store.Log) *apitypes.Log {
	if l == nil {
		return nil
	}
	return &apitypes.Log{
		ID:        l.ID,
		SessionID: l.SessionID,
		TaskID:    l.TaskID,
		RunID:     l.RunID,
		RunnerID:  l.RunnerID,
		Stream:    l.Stream,
		Level:     l.Level,
		Content:   l.Content,
		Sequence:  l.Sequence,
		Metadata:  decodeStringMap(l.Metadata),
		CreatedAt: l.CreatedAt,
	}
}

// toListResponse maps a store list result onto the wire envelope, applying
// convert to every item. Pagination metadata is carried through verbatim —
// dropping has_more/total_count on the way out is why no page in the UI could
// paginate.
func toListResponse[S any, D any](res *store.ListResult[S], convert func(*S) *D) *apitypes.ListResponse[D] {
	out := &apitypes.ListResponse[D]{Items: []*D{}}
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

// toSliceResponse wraps an unpaginated slice in the list envelope, so every
// list endpoint returns the same shape whether or not it supports cursors.
func toSliceResponse[S any, D any](items []*S, convert func(*S) *D) *apitypes.ListResponse[D] {
	out := &apitypes.ListResponse[D]{Items: []*D{}}
	for _, item := range items {
		out.Items = append(out.Items, convert(item))
	}
	out.TotalCount = int64(len(out.Items))
	return out
}
