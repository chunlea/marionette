package api

import (
	"github.com/chunlea/marionette/pkg/openapi"
	"github.com/chunlea/marionette/pkg/server/api/apitypes"
)

// The route table is the hand-maintained half of the public OpenAPI document:
// summaries, status codes and query parameters, which no amount of reflection
// can recover from Go types. Schemas come from apitypes.
//
// It is kept honest by TestOpenAPICoversEveryRoute, which walks the chi router
// the server actually serves and fails if a route is missing here - the reason
// roughly forty live endpoints were absent from the previous hand-written
// specs.
//
// The document types, schema reflection and query-parameter helpers used to be
// duplicated here; they now come from pkg/openapi, which the admin document
// also renders through. Two copies of a generator is two places for the
// contract to drift.

// apiSecurity is how a caller authenticates against the public API.
var apiSecurity = openapi.SecurityScheme{
	Name: "ApiKeyAuth",
	Fields: map[string]string{
		"type":         "http",
		"scheme":       "bearer",
		"description":  "An API key minted by the admin API.",
		"bearerFormat": "mk_...",
		"unauthorized": "The API key is missing, malformed, revoked or expired.",
	},
}

// publicRoutes is every route the public API server serves.
func publicRoutes() []openapi.Route {
	return []openapi.Route{
		// ---- Service ----
		{
			Method: "GET", Path: "/health", Tag: "Service",
			Summary:  "Liveness probe",
			Success:  200,
			Response: apitypes.HealthStatus{},
		},
		{
			Method: "GET", Path: "/healthz", Tag: "Service",
			Summary:  "Liveness probe (Kubernetes spelling)",
			Success:  200,
			Response: apitypes.HealthStatus{},
		},
		{
			Method: "GET", Path: "/openapi.yaml", Tag: "Service",
			Summary:             "This document",
			Success:             200,
			SuccessDescription:  "The OpenAPI description of this API.",
			ResponseContentType: "application/yaml",
		},
		{
			Method: "GET", Path: "/docs", Tag: "Service",
			Summary:             "Interactive API documentation",
			Success:             200,
			SuccessDescription:  "An HTML page rendering this document.",
			ResponseContentType: "text/html",
		},

		// ---- Sessions ----
		{
			Method: "POST", Path: "/api/v1/sessions", Tag: "Sessions",
			Summary:     "Create a session",
			Description: "Creates a session and, unless an existing workspace is given, the workspace behind it. The session starts pending and becomes active once a runner is attached.",
			Secured:     true,
			Scopes:      []string{"sessions:write"},
			Request:     CreateSessionOptions{},
			Success:     201, Response: apitypes.Session{},
		},
		{
			Method: "GET", Path: "/api/v1/sessions", Tag: "Sessions",
			Summary: "List sessions",
			Secured: true,
			Scopes:  []string{"sessions:read"},
			Query: openapi.WithQuery(openapi.PaginationQuery(),
				openapi.RepeatedQuery("status", "Filter by session status."),
				openapi.StringQuery("agent", "Filter by agent type."),
				openapi.StringQuery("lifecycle_mode", "Filter by lifecycle mode."),
				openapi.LabelQuery("Filter by label, e.g. labels[env]=prod."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.Session]{},
		},
		{
			Method: "GET", Path: "/api/v1/sessions/{sessionID}", Tag: "Sessions",
			Summary: "Get a session",
			Secured: true,
			Scopes:  []string{"sessions:read"},
			Success: 200, Response: apitypes.Session{},
		},
		{
			Method: "DELETE", Path: "/api/v1/sessions/{sessionID}", Tag: "Sessions",
			Summary:     "Terminate a session",
			Description: "Ends the session and releases its resources. Terminated sessions cannot be resumed.",
			Secured:     true,
			Scopes:      []string{"sessions:write"},
			Success:     204,
		},
		{
			Method: "POST", Path: "/api/v1/sessions/{sessionID}/suspend", Tag: "Sessions",
			Summary:     "Suspend a session",
			Description: "Releases the runner while preserving the workspace and the agent context, so the session can be resumed later.",
			Secured:     true,
			Scopes:      []string{"sessions:write"},
			Success:     204,
		},
		{
			Method: "POST", Path: "/api/v1/sessions/{sessionID}/resume", Tag: "Sessions",
			Summary:     "Resume a suspended session",
			Description: "Acquires a runner, restores the workspace and context, and delivers any permission responses that arrived while the session was suspended.",
			Secured:     true,
			Scopes:      []string{"sessions:write"},
			Success:     204,
		},

		// ---- Tasks ----
		{
			Method: "POST", Path: "/api/v1/tasks", Tag: "Tasks",
			Summary:     "Create a task",
			Description: "Queues a prompt in a session. If the session is active the task is dispatched immediately; if it is pending or suspended, creating the task also brings it up.",
			Secured:     true,
			Scopes:      []string{"tasks:write"},
			Request:     CreateTaskOptions{},
			Success:     201, Response: apitypes.Task{},
		},
		{
			Method: "GET", Path: "/api/v1/tasks", Tag: "Tasks",
			Summary: "List tasks",
			Secured: true,
			Scopes:  []string{"tasks:read"},
			Query: openapi.WithQuery(openapi.PaginationQuery(),
				openapi.StringQuery("session_id", "Filter by session."),
				openapi.RepeatedQuery("status", "Filter by task status."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.Task]{},
		},
		{
			Method: "GET", Path: "/api/v1/tasks/{taskID}", Tag: "Tasks",
			Summary: "Get a task",
			Secured: true,
			Scopes:  []string{"tasks:read"},
			Success: 200, Response: apitypes.Task{},
		},
		{
			Method: "POST", Path: "/api/v1/tasks/{taskID}/execute", Tag: "Tasks",
			Summary:     "Execute a pending task",
			Description: "Dispatches a task that is still pending. Returns as soon as the runner has accepted the work.",
			Secured:     true,
			Scopes:      []string{"tasks:write"},
			Success:     202, Response: apitypes.TaskExecutionAccepted{},
		},
		{
			Method: "POST", Path: "/api/v1/tasks/{taskID}/cancel", Tag: "Tasks",
			Summary: "Cancel a task",
			Secured: true,
			Scopes:  []string{"tasks:write"},
			Success: 204,
		},
		{
			Method: "POST", Path: "/api/v1/tasks/{taskID}/retry", Tag: "Tasks",
			Summary:     "Retry a failed task",
			Description: "Starts a new run of a failed task. Fails with 409 once max_retries is reached.",
			Secured:     true,
			Scopes:      []string{"tasks:write"},
			Success:     204,
		},
		{
			Method: "GET", Path: "/api/v1/tasks/{taskID}/runs", Tag: "Tasks",
			Summary:     "List task runs",
			Description: "Lists the execution attempts of a task, oldest attempt first. The task itself only carries the latest status, so this is where a retry that failed before a later attempt succeeded is visible.",
			Secured:     true,
			Scopes:      []string{"tasks:read"},
			Query: openapi.WithQuery(openapi.PaginationQuery(),
				openapi.RepeatedQuery("status", "Filter by run status."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.TaskRun]{},
		},
		{
			Method: "GET", Path: "/api/v1/tasks/{taskID}/logs", Tag: "Tasks",
			Summary: "List task logs",
			Secured: true,
			Scopes:  []string{"tasks:read"},
			Query: openapi.WithQuery(openapi.PaginationQuery(),
				openapi.RepeatedQuery("level", "Filter by log level."),
				openapi.RepeatedQuery("stream", "Filter by output stream."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.Log]{},
		},

		// ---- Runners ----
		{
			Method: "GET", Path: "/api/v1/runners", Tag: "Runners",
			Summary:     "List runners",
			Description: "Runners are managed by the server; this view is read-only. Provisioning lives in the admin API.",
			Secured:     true,
			Scopes:      []string{"runners:read"},
			Query: openapi.WithQuery(openapi.PaginationQuery(),
				openapi.RepeatedQuery("status", "Filter by runner status."),
				openapi.StringQuery("pool_name", "Filter by pool."),
				openapi.LabelQuery("Filter by label, e.g. labels[env]=prod."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.Runner]{},
		},
		{
			Method: "GET", Path: "/api/v1/runners/{runnerID}", Tag: "Runners",
			Summary: "Get a runner",
			Secured: true,
			Scopes:  []string{"runners:read"},
			Success: 200, Response: apitypes.Runner{},
		},

		// ---- Permissions ----
		{
			Method: "GET", Path: "/api/v1/permissions", Tag: "Permissions",
			Summary:     "List permission requests",
			Description: "Permission requests never expire on their own: they stay pending until approved, denied, or cancelled with the task.",
			Secured:     true,
			Scopes:      []string{"permissions:read"},
			Query: openapi.WithQuery(openapi.PaginationQuery(),
				openapi.StringQuery("session_id", "Filter by session."),
				openapi.StringQuery("task_id", "Filter by task."),
				openapi.RepeatedQuery("status", "Filter by request status."),
				openapi.RepeatedQuery("risk_level", "Filter by risk level."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.PermissionRequest]{},
		},
		{
			Method: "GET", Path: "/api/v1/permissions/{permissionID}", Tag: "Permissions",
			Summary: "Get a permission request",
			Secured: true,
			Scopes:  []string{"permissions:read"},
			Success: 200, Response: apitypes.PermissionRequest{},
		},
		{
			Method: "POST", Path: "/api/v1/permissions/{permissionID}/approve", Tag: "Permissions",
			Summary:     "Approve a permission request",
			Description: "May be called while the session is suspended; the response is delivered when the session resumes.",
			Secured:     true,
			Scopes:      []string{"permissions:write"},
			Request:     ApproveOptions{},
			Success:     204,
		},
		{
			Method: "POST", Path: "/api/v1/permissions/{permissionID}/deny", Tag: "Permissions",
			Summary: "Deny a permission request",
			Secured: true,
			Scopes:  []string{"permissions:write"},
			Request: DenyOptions{},
			Success: 204,
		},

		// ---- Workspaces ----
		{
			Method: "POST", Path: "/api/v1/workspaces", Tag: "Workspaces",
			Summary: "Create a workspace",
			Secured: true,
			Scopes:  []string{"workspaces:write"},
			Request: CreateWorkspaceOptions{},
			Success: 201, Response: apitypes.Workspace{},
		},
		{
			Method: "GET", Path: "/api/v1/workspaces", Tag: "Workspaces",
			Summary: "List workspaces",
			Secured: true,
			Scopes:  []string{"workspaces:read"},
			Query:   openapi.PaginationQuery(),
			Success: 200, Response: apitypes.ListResponse[apitypes.Workspace]{},
		},
		{
			Method: "GET", Path: "/api/v1/workspaces/{workspaceID}", Tag: "Workspaces",
			Summary: "Get a workspace",
			Secured: true,
			Scopes:  []string{"workspaces:read"},
			Success: 200, Response: apitypes.Workspace{},
		},
		{
			Method: "PATCH", Path: "/api/v1/workspaces/{workspaceID}", Tag: "Workspaces",
			Summary: "Update a workspace",
			Secured: true,
			Scopes:  []string{"workspaces:write"},
			Request: UpdateWorkspaceOptions{},
			Success: 200, Response: apitypes.Workspace{},
		},
		{
			Method: "DELETE", Path: "/api/v1/workspaces/{workspaceID}", Tag: "Workspaces",
			Summary:     "Delete a workspace",
			Description: "Soft-deletes the workspace. Fails with 409 while a session still uses it.",
			Secured:     true,
			Scopes:      []string{"workspaces:write"},
			Success:     204,
		},

		// ---- Scheduled tasks ----
		{
			Method: "POST", Path: "/api/v1/scheduled-tasks", Tag: "Scheduled Tasks",
			Summary: "Create a scheduled task",
			Secured: true,
			Scopes:  []string{"scheduled-tasks:write"},
			Request: CreateScheduledTaskOptions{},
			Success: 201, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "GET", Path: "/api/v1/scheduled-tasks", Tag: "Scheduled Tasks",
			Summary: "List scheduled tasks",
			Secured: true,
			Scopes:  []string{"scheduled-tasks:read"},
			Query: openapi.WithQuery(openapi.PaginationQuery(),
				openapi.StringQuery("session_id", "Filter by session."),
				openapi.RepeatedQuery("status", "Filter by scheduled task status."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.ScheduledTask]{},
		},
		{
			Method: "GET", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}", Tag: "Scheduled Tasks",
			Summary: "Get a scheduled task",
			Secured: true,
			Scopes:  []string{"scheduled-tasks:read"},
			Success: 200, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "PATCH", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}", Tag: "Scheduled Tasks",
			Summary: "Update a scheduled task",
			Secured: true,
			Scopes:  []string{"scheduled-tasks:write"},
			Request: UpdateScheduledTaskOptions{},
			Success: 200, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "DELETE", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}", Tag: "Scheduled Tasks",
			Summary: "Delete a scheduled task",
			Secured: true,
			Scopes:  []string{"scheduled-tasks:write"},
			Success: 204,
		},
		{
			Method: "POST", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}/pause", Tag: "Scheduled Tasks",
			Summary: "Pause a scheduled task",
			Secured: true,
			Scopes:  []string{"scheduled-tasks:write"},
			Success: 200, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "POST", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}/resume", Tag: "Scheduled Tasks",
			Summary: "Resume a paused scheduled task",
			Secured: true,
			Scopes:  []string{"scheduled-tasks:write"},
			Success: 200, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "POST", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}/trigger", Tag: "Scheduled Tasks",
			Summary:     "Trigger a scheduled task now",
			Description: "Creates the task the schedule would have created, without waiting for the next occurrence.",
			Secured:     true,
			Scopes:      []string{"scheduled-tasks:write"},
			Success:     200, Response: apitypes.Task{},
		},

		// ---- Tunnels ----
		{
			Method: "POST", Path: "/api/v1/sessions/{sessionID}/tunnels", Tag: "Tunnels",
			Summary:     "Open a tunnel into a session",
			Description: "Forwards a port inside the runner through the API server. The response is the only place the tunnel token is readable.",
			Secured:     true,
			Scopes:      []string{"tunnels:write"},
			Request:     CreateTunnelOptions{},
			Success:     201, Response: apitypes.Tunnel{},
		},
		{
			Method: "GET", Path: "/api/v1/sessions/{sessionID}/tunnels", Tag: "Tunnels",
			Summary: "List a session's tunnels",
			Secured: true,
			Scopes:  []string{"tunnels:read"},
			Success: 200, Response: apitypes.ListResponse[apitypes.Tunnel]{},
		},
		{
			Method: "GET", Path: "/api/v1/tunnels/{tunnelID}", Tag: "Tunnels",
			Summary: "Get a tunnel",
			Secured: true,
			Scopes:  []string{"tunnels:read"},
			Success: 200, Response: apitypes.Tunnel{},
		},
		{
			Method: "DELETE", Path: "/api/v1/tunnels/{tunnelID}", Tag: "Tunnels",
			Summary: "Close a tunnel",
			Secured: true,
			Scopes:  []string{"tunnels:write"},
			Success: 204,
		},
		{
			Method: "GET", Path: "/tunnels/{tunnelID}/", Tag: "Tunnels",
			OperationID:         "openTunnel",
			Summary:             "Tunnel entry point",
			Description:         "Serves the tunnelled application. Authenticated by the tunnel token (Authorization: Bearer, the X-Marionette-Tunnel-Token header, or HTTP basic with the token as the password) unless the tunnel is public.",
			Success:             200,
			SuccessDescription:  "Whatever the tunnelled service returned.",
			ResponseContentType: "*/*",
		},
		{
			Method: "GET", Path: "/tunnels/{tunnelID}/{path}", Tag: "Tunnels",
			OperationID:         "proxyThroughTunnel",
			Summary:             "Tunnel passthrough",
			Description:         "Proxies any path, and any method, to the tunnelled service. WebSocket upgrades are relayed too.",
			Success:             200,
			SuccessDescription:  "Whatever the tunnelled service returned.",
			ResponseContentType: "*/*",
		},

		// ---- Streaming ----
		{
			Method: "GET", Path: "/api/v1/logs/{taskID}/stream", Tag: "Streaming",
			Summary:     "Stream task logs over WebSocket",
			Description: "Upgrades to a WebSocket that emits one JSON log message per line. Browsers cannot set headers on a WebSocket handshake, so this route also accepts the API key as a ?token= query parameter.",
			Secured:     true,
			Scopes:      []string{"tasks:read"},
			Query:       []openapi.Parameter{openapi.StringQuery("token", "API key, for clients that cannot set an Authorization header.")},
			Success:     101,
		},
		{
			Method: "GET", Path: "/api/v1/events", Tag: "Streaming",
			Summary:     "Stream server events over WebSocket",
			Description: "Emits permission requests and session, task and runner state changes.",
			Secured:     true,
			Scopes:      []string{"events:read"},
			Query: []openapi.Parameter{
				openapi.RepeatedQuery("event_type", "Only deliver these event types."),
				openapi.StringQuery("labels", "JSON object of labels to filter on."),
				openapi.StringQuery("token", "API key, for clients that cannot set an Authorization header."),
			},
			Success: 101,
		},
		{
			Method: "GET", Path: "/api/v1/streams/{streamID}/ws", Tag: "Streaming",
			Summary:     "Stream browser frames over WebSocket",
			Description: "Sends rendered frames to the client and accepts input events back.",
			Secured:     true,
			Scopes:      []string{"streams:read"},
			Query:       []openapi.Parameter{openapi.StringQuery("token", "API key, for clients that cannot set an Authorization header.")},
			Success:     101,
		},
	}
}
