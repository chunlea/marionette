package admin

import (
	"github.com/chunlea/marionette/pkg/observability/health"
	"github.com/chunlea/marionette/pkg/openapi"
	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
)

// The route table is the hand-maintained half of the admin OpenAPI document:
// summaries, status codes and query parameters, which no amount of reflection
// can recover from Go types. Schemas come from admintypes.
//
// TestAdminOpenAPICoversEveryRoute walks the chi router the server actually
// serves and fails on any route missing from here. The previous hand-written
// document described 9 of these; there are more than fifty.

// adminSecurity is the only way into the admin API.
//
// The document used to also offer `Authorization: Bearer mk_...` with admin
// scopes. The middleware has only ever checked basic auth, so the document was
// advertising a door that does not exist.
var adminSecurity = openapi.SecurityScheme{
	Name: "basicAuth",
	Fields: map[string]string{
		"type":         "http",
		"scheme":       "basic",
		"description":  "Credentials from MARIONETTE_UI_USERNAME and MARIONETTE_UI_PASSWORD.",
		"unauthorized": "Basic auth credentials are missing or wrong.",
	},
}

// secured marks a route as needing the admin credentials. The admin API has no
// scopes: holding the credentials is total authority over the deployment.
func secured(r openapi.Route) openapi.Route {
	r.Secured = true
	return r
}

// adminListQuery is the filter set every admin listing accepts.
func adminListQuery(extra ...openapi.Parameter) []openapi.Parameter {
	return openapi.WithQuery(
		[]openapi.Parameter{
			openapi.IntQuery("limit", "Maximum number of items to return. Defaults to 50, capped at 100."),
			openapi.StringQuery("cursor", "Opaque cursor from a previous response's next_cursor."),
			openapi.LabelQuery(`Filter by labels, as "key=value" pairs separated by commas, e.g. env=prod,team=backend.`),
		},
		extra...,
	)
}

// adminRoutes is every route the admin server serves.
func adminRoutes() []openapi.Route {
	routes := []openapi.Route{
		// ---- Service ----
		{
			Method: "GET", Path: "/health", Tag: "Service",
			Summary: "Liveness probe", Success: 200, Response: admintypes.Health{},
		},
		{
			Method: "GET", Path: "/healthz", Tag: "Service",
			Summary: "Liveness probe (Kubernetes spelling)", Success: 200, Response: admintypes.Health{},
		},
		{
			Method: "GET", Path: "/health/live", Tag: "Service",
			Summary:     "Liveness probe with per-check detail",
			Description: "Served only when a health service is configured.",
			Success:     200, Response: health.Response{},
		},
		{
			Method: "GET", Path: "/health/ready", Tag: "Service",
			Summary:     "Readiness probe",
			Description: "Reports the dependencies the server needs before it can serve traffic.",
			Success:     200, Response: health.Response{},
		},
		{
			Method: "GET", Path: "/api/status", Tag: "Service",
			Summary:     "Status of every service in the process",
			Description: "The server, admin, gRPC and metrics listeners, as registered at start-up.",
			Success:     200, Response: admintypes.Status{},
		},
		{
			Method: "GET", Path: "/openapi.yaml", Tag: "Service",
			Summary: "This document", Success: 200,
			SuccessDescription:  "The OpenAPI description of the admin API.",
			ResponseContentType: "application/yaml",
		},
		{
			Method: "GET", Path: "/docs", Tag: "Service",
			Summary: "Interactive API documentation", Success: 200,
			SuccessDescription:  "An HTML page rendering this document.",
			ResponseContentType: "text/html",
		},
		{
			Method: "GET", Path: "/{path}", Tag: "Service",
			OperationID: "getDashboard",
			Summary:     "The dashboard",
			Description: "Everything not matched above serves the embedded single-page app, " +
				"falling back to index.html so client-side routes deep-link. This origin also " +
				"forwards /api/v1 to the public API, so the browser has one origin for both.",
			Success: 200, SuccessDescription: "The dashboard, or one of its assets.",
			ResponseContentType: "text/html",
		},

		// ---- API keys ----
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/keys", Tag: "API Keys",
			Summary:     "Create an API key",
			Description: "The response is the only time the token is readable.",
			Request:     CreateAPIKeyRequest{},
			Success:     201, Response: admintypes.CreatedAPIKey{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/keys", Tag: "API Keys",
			Summary: "List API keys", Query: adminListQuery(),
			Success: 200, Response: admintypes.ListResponse[admintypes.APIKey]{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/keys/{keyID}", Tag: "API Keys",
			Summary: "Get an API key", Success: 200, Response: admintypes.APIKey{},
		}),
		secured(openapi.Route{
			Method: "DELETE", Path: "/admin/api/v1/keys/{keyID}", Tag: "API Keys",
			Summary:     "Revoke an API key",
			Description: "Revocation is immediate and permanent; keys are never deleted, so the audit trail survives.",
			Request:     RevokeAPIKeyRequest{},
			Success:     204,
		}),

		// ---- Agent configs ----
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/agent-configs", Tag: "Agent Configs",
			Summary:     "Create an agent config",
			Description: "The agent's API key is encrypted at rest and never returned.",
			Request:     CreateAgentConfigRequest{},
			Success:     201, Response: admintypes.AgentConfig{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/agent-configs", Tag: "Agent Configs",
			Summary: "List agent configs",
			Query:   adminListQuery(openapi.StringQuery("agent", "Filter by agent type.")),
			Success: 200, Response: admintypes.ListResponse[admintypes.AgentConfig]{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/agent-configs/{configID}", Tag: "Agent Configs",
			Summary: "Get an agent config", Success: 200, Response: admintypes.AgentConfig{},
		}),
		secured(openapi.Route{
			Method: "PUT", Path: "/admin/api/v1/agent-configs/{configID}", Tag: "Agent Configs",
			Summary: "Update an agent config", Request: UpdateAgentConfigRequest{},
			Success: 200, Response: admintypes.AgentConfig{},
		}),
		secured(openapi.Route{
			Method: "DELETE", Path: "/admin/api/v1/agent-configs/{configID}", Tag: "Agent Configs",
			Summary: "Delete an agent config", Success: 204,
		}),

		// ---- Provider configs ----
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/provider-configs", Tag: "Provider Configs",
			Summary:     "Create a provider config",
			Description: "The config block is provider-specific and is stored and returned as given.",
			Request:     CreateProviderConfigRequest{},
			Success:     201, Response: admintypes.ProviderConfig{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/provider-configs", Tag: "Provider Configs",
			Summary: "List provider configs",
			Query:   adminListQuery(openapi.StringQuery("provider", "Filter by provider kind, e.g. docker.")),
			Success: 200, Response: admintypes.ListResponse[admintypes.ProviderConfig]{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/provider-configs/{configID}", Tag: "Provider Configs",
			Summary: "Get a provider config", Success: 200, Response: admintypes.ProviderConfig{},
		}),
		secured(openapi.Route{
			Method: "PUT", Path: "/admin/api/v1/provider-configs/{configID}", Tag: "Provider Configs",
			Summary: "Update a provider config", Request: UpdateProviderConfigRequest{},
			Success: 200, Response: admintypes.ProviderConfig{},
		}),
		secured(openapi.Route{
			Method: "DELETE", Path: "/admin/api/v1/provider-configs/{configID}", Tag: "Provider Configs",
			Summary: "Delete a provider config", Success: 204,
		}),

		// ---- Profiles ----
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/profiles", Tag: "Profiles",
			Summary:     "Create a profile",
			Description: "A profile is the reusable runner shape a session asks for by id.",
			Request:     CreateProfileRequest{},
			Success:     201, Response: admintypes.Profile{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/profiles", Tag: "Profiles",
			Summary: "List profiles",
			Query: adminListQuery(
				openapi.StringQuery("provider_config_id", "Filter by provider config."),
				openapi.BoolQuery("include_builtin", "Include the profiles the server ships."),
			),
			Success: 200, Response: admintypes.ListResponse[admintypes.Profile]{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/profiles/{profileID}", Tag: "Profiles",
			Summary: "Get a profile", Success: 200, Response: admintypes.Profile{},
		}),
		secured(openapi.Route{
			Method: "PUT", Path: "/admin/api/v1/profiles/{profileID}", Tag: "Profiles",
			Summary:     "Update a profile",
			Description: "Built-in profiles cannot be edited.",
			Request:     UpdateProfileRequest{},
			Success:     200, Response: admintypes.Profile{},
		}),
		secured(openapi.Route{
			Method: "DELETE", Path: "/admin/api/v1/profiles/{profileID}", Tag: "Profiles",
			Summary: "Delete a profile", Success: 204,
		}),

		// ---- Runners ----
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/runners/spawn", Tag: "Runners",
			Summary:     "Spawn a runner",
			Description: "Provisions a runner through the given provider config. Pool runners join by themselves and are not spawned here.",
			Request:     SpawnRunnerRequest{},
			Success:     201, Response: admintypes.Runner{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/runners", Tag: "Runners",
			Summary:     "List runners",
			Description: "The operator's view, which unlike the public one names the provider behind each runner.",
			Query: adminListQuery(
				openapi.CSVQuery("status", "Filter by status, comma-separated."),
				openapi.StringQuery("pool_name", "Filter by pool."),
			),
			Success: 200, Response: admintypes.ListResponse[admintypes.Runner]{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/runners/{runnerID}", Tag: "Runners",
			Summary: "Get a runner", Success: 200, Response: admintypes.Runner{},
		}),
		secured(openapi.Route{
			Method: "DELETE", Path: "/admin/api/v1/runners/{runnerID}", Tag: "Runners",
			Summary:     "Destroy a runner",
			Description: "Terminates the runner through its provider and removes it.",
			Success:     204,
		}),

		// ---- Runner tokens ----
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/runner-tokens", Tag: "Runner Tokens",
			Summary:     "Create a runner token",
			Description: "The credential a pool runner presents when it joins. The response is the only time it is readable.",
			Request:     CreateRunnerTokenRequest{},
			Success:     201, Response: admintypes.CreatedRunnerToken{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/runner-tokens", Tag: "Runner Tokens",
			Summary: "List runner tokens",
			Query: adminListQuery(
				openapi.StringQuery("pool_name", "Filter by pool."),
				openapi.CSVQuery("status", "Filter by status, comma-separated."),
				openapi.BoolQuery("include_revoked", "Include revoked tokens."),
			),
			Success: 200, Response: admintypes.ListResponse[admintypes.RunnerToken]{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/runner-tokens/{tokenID}", Tag: "Runner Tokens",
			Summary: "Get a runner token", Success: 200, Response: admintypes.RunnerToken{},
		}),
		secured(openapi.Route{
			Method: "DELETE", Path: "/admin/api/v1/runner-tokens/{tokenID}", Tag: "Runner Tokens",
			Summary: "Revoke a runner token", Request: RevokeRunnerTokenRequest{}, Success: 204,
		}),
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/runner-tokens/{tokenID}/rotate", Tag: "Runner Tokens",
			Summary: "Rotate a runner token",
			Description: "Issues a replacement and gives the runner until rotation_deadline to " +
				"start using it. The response is the only time the new token is readable.",
			Success: 200, Response: admintypes.CreatedRunnerToken{},
		}),

		// ---- Sessions ----
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/sessions/{sessionID}/activate", Tag: "Sessions",
			Summary:     "Attach a runner to a pending session",
			Description: "An operator escape hatch; ordinary session lifecycle runs through the public API.",
			Request:     admintypes.ActivateSessionRequest{},
			Success:     200, Response: admintypes.Accepted{},
		}),
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/sessions/{sessionID}/suspend", Tag: "Sessions",
			Summary: "Suspend a session", Request: admintypes.SuspendSessionRequest{},
			Success: 200, Response: admintypes.Accepted{},
		}),

		// ---- Action logs ----
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/action-logs", Tag: "Action Logs",
			Summary:     "List audited actions",
			Description: "Every state-changing call, who made it, and whether it succeeded.",
			Query: openapi.WithQuery(
				[]openapi.Parameter{
					openapi.IntQuery("limit", "Maximum number of items to return. Defaults to 50, capped at 100."),
					openapi.StringQuery("cursor", "Opaque cursor from a previous response's next_cursor."),
				},
				openapi.StringQuery("actor_type", "Filter by actor kind: user, api_key, system or runner."),
				openapi.StringQuery("actor_id", "Filter by actor."),
				openapi.StringQuery("action", "Exact action match, e.g. session.create."),
				openapi.StringQuery("action_prefix", "Action prefix match, e.g. permission."),
				openapi.StringQuery("resource_type", "Filter by resource kind."),
				openapi.StringQuery("resource_id", "Filter by resource."),
				openapi.StringQuery("session_id", "Filter by session."),
				openapi.StringQuery("task_id", "Filter by task."),
				openapi.BoolQuery("success", "Only successes, or only failures."),
				openapi.TimeQuery("from", "Only entries at or after this time."),
				openapi.TimeQuery("to", "Only entries at or before this time."),
			),
			Success: 200, Response: admintypes.ListResponse[admintypes.ActionLog]{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/action-logs/{logID}", Tag: "Action Logs",
			Summary: "Get an audited action", Success: 200, Response: admintypes.ActionLog{},
		}),

		// ---- Webhooks ----
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/webhooks", Tag: "Webhooks",
			Summary:     "Create a webhook",
			Description: "The signing secret in the response is readable only here and on rotation.",
			Request:     CreateWebhookRequest{},
			Success:     201, Response: admintypes.CreatedWebhook{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/webhooks", Tag: "Webhooks",
			Summary: "List webhooks",
			Query: adminListQuery(
				openapi.BoolQuery("is_active", "Only active webhooks."),
			),
			Success: 200, Response: admintypes.ListResponse[admintypes.Webhook]{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/webhooks/{webhookID}", Tag: "Webhooks",
			Summary: "Get a webhook", Success: 200, Response: admintypes.Webhook{},
		}),
		secured(openapi.Route{
			Method: "PUT", Path: "/admin/api/v1/webhooks/{webhookID}", Tag: "Webhooks",
			Summary: "Update a webhook", Request: UpdateWebhookRequest{},
			Success: 200, Response: admintypes.Webhook{},
		}),
		secured(openapi.Route{
			Method: "DELETE", Path: "/admin/api/v1/webhooks/{webhookID}", Tag: "Webhooks",
			Summary: "Delete a webhook", Success: 204,
		}),
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/webhooks/{webhookID}/rotate-secret", Tag: "Webhooks",
			Summary:     "Rotate a webhook's signing secret",
			Description: "Deliveries are signed with the new secret from the next event onward.",
			Success:     200, Response: admintypes.RotatedWebhookSecret{},
		}),

		// ---- Webhook events ----
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/webhook-events", Tag: "Webhook Events",
			Summary: "List delivery attempts",
			Query: openapi.WithQuery(
				[]openapi.Parameter{
					openapi.IntQuery("limit", "Maximum number of items to return. Defaults to 50, capped at 100."),
					openapi.StringQuery("cursor", "Opaque cursor from a previous response's next_cursor."),
				},
				openapi.StringQuery("webhook_id", "Filter by webhook."),
				openapi.StringQuery("status", "Filter by delivery status."),
				openapi.StringQuery("event_type", "Filter by event type."),
			),
			Success: 200, Response: admintypes.ListResponse[admintypes.WebhookEvent]{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/webhook-events/{eventID}", Tag: "Webhook Events",
			Summary: "Get a delivery attempt", Success: 200, Response: admintypes.WebhookEvent{},
		}),
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/webhook-events/{eventID}/retry", Tag: "Webhook Events",
			Summary:     "Retry a failed delivery",
			Description: "Queues the event again immediately, without waiting for the backoff.",
			Success:     200, Response: admintypes.Accepted{},
		}),

		// ---- Streaming (frozen subsystem) ----
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/streams", Tag: "Streaming",
			Summary:     "List desktop streams",
			Description: streamingFrozenNote,
			Query: []openapi.Parameter{
				openapi.StringQuery("session_id", "Filter by session."),
				openapi.StringQuery("runner_id", "Filter by runner."),
				openapi.StringQuery("tenant_id", "Filter by tenant."),
				openapi.StringQuery("type", "Filter by stream type."),
				openapi.StringQuery("state", "Filter by stream state."),
				openapi.BoolQuery("active_only", "Only streams that are starting or active."),
				openapi.IntQuery("limit", "Maximum number of items to return."),
				openapi.IntQuery("offset", "Number of items to skip. This listing pages by offset, not by cursor."),
			},
			Success: 200, Response: admintypes.ListResponse[StreamResponse]{},
		}),
		secured(openapi.Route{
			Method: "POST", Path: "/admin/api/v1/streams", Tag: "Streaming",
			Summary: "Start a desktop stream", Description: streamingFrozenNote,
			Request: StreamRequest{},
			Success: 201, Response: StreamResponse{},
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/streams/{streamID}", Tag: "Streaming",
			Summary: "Get a desktop stream", Description: streamingFrozenNote,
			Success: 200, Response: StreamResponse{},
		}),
		secured(openapi.Route{
			Method: "DELETE", Path: "/admin/api/v1/streams/{streamID}", Tag: "Streaming",
			Summary: "Stop a desktop stream", Description: streamingFrozenNote,
			Success: 204,
		}),
		secured(openapi.Route{
			Method: "GET", Path: "/admin/api/v1/signaling", Tag: "Streaming",
			Summary: "WebRTC signalling",
			Description: streamingFrozenNote + "\n\nA browser cannot attach basic auth to a WebSocket " +
				"handshake, so the dashboard cannot reach this endpoint at all today; unfreezing " +
				"desktop streaming means giving it a query-token path like the public log stream has.",
			Success: 101,
		}),
	}
	return routes
}

const streamingFrozenNote = "Part of the frozen streaming subsystem: served only when the " +
	"streaming handlers are configured, and hidden in the dashboard unless it is built with " +
	"VITE_ENABLE_STREAMING."
