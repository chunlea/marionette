package api

import (
	"github.com/chunlea/marionette/pkg/server/api/apitypes"
)

// The route table below is the hand-maintained half of the OpenAPI document:
// summaries, status codes, query parameters and required scopes, which no
// amount of reflection can recover from Go types. Schemas come from the types
// themselves (openapi_schema.go).
//
// It is kept honest by TestOpenAPICoversEveryRoute, which walks the chi router
// the server actually serves and fails if a route is missing here — the reason
// roughly forty live endpoints were absent from the previous hand-written
// specs.

// oaParameter is a path or query parameter.
type oaParameter struct {
	Name        string    `yaml:"name"`
	In          string    `yaml:"in"`
	Description string    `yaml:"description,omitempty"`
	Required    bool      `yaml:"required,omitempty"`
	Explode     *bool     `yaml:"explode,omitempty"`
	Style       string    `yaml:"style,omitempty"`
	Schema      *oaSchema `yaml:"schema"`
}

// routeSpec describes one operation of the public API.
type routeSpec struct {
	Method string
	// Path is the OpenAPI path, using the same {param} syntax as chi.
	Path string
	Tag  string
	// OperationID overrides the identifier derived from the method and path.
	// Only needed where two routes would derive the same one.
	OperationID string
	Summary     string
	Description string
	// Scope is the API key scope the route requires. Empty means the route is
	// served without authentication.
	Scope string
	// Query lists the query parameters the handler actually reads.
	Query []oaParameter
	// Request is a zero value of the request body type, or nil.
	Request any
	// Success is the status code of the happy path.
	Success int
	// SuccessDescription documents that status code.
	SuccessDescription string
	// Response is a zero value of the response body type, or nil for 204.
	Response any
	// ResponseContentType defaults to application/json.
	ResponseContentType string
}

// explodeRepeatedKeys is the OpenAPI spelling of ?key=a&key=b.
var explodeRepeatedKeys = true

// repeatedQuery describes a filter that may be given more than once, e.g.
// ?status=pending&status=running. Go reads these with r.URL.Query()[key], so
// the spec has to say explode/form — an axios client left on its defaults
// sends status[]=pending instead and every filter is silently ignored.
func repeatedQuery(name, description string) oaParameter {
	return oaParameter{
		Name:        name,
		In:          "query",
		Description: description,
		Style:       "form",
		Explode:     &explodeRepeatedKeys,
		Schema:      &oaSchema{Type: "array", Items: &oaSchema{Type: "string"}},
	}
}

func stringQuery(name, description string) oaParameter {
	return oaParameter{Name: name, In: "query", Description: description, Schema: &oaSchema{Type: "string"}}
}

func intQuery(name, description string) oaParameter {
	return oaParameter{Name: name, In: "query", Description: description, Schema: &oaSchema{Type: "integer", Format: "int32"}}
}

// paginationQuery is the cursor pagination pair every list endpoint accepts.
func paginationQuery() []oaParameter {
	return []oaParameter{
		intQuery("limit", "Maximum number of items to return. Defaults to 50."),
		stringQuery("cursor", "Opaque cursor from a previous response's next_cursor."),
	}
}

// labelQuery documents the labels[key]=value filter form.
func labelQuery() oaParameter {
	return oaParameter{
		Name:        "labels[key]",
		In:          "query",
		Description: "Filter by label. Repeat with different keys to AND several labels, e.g. labels[env]=prod.",
		Schema:      &oaSchema{Type: "string"},
	}
}

func withQuery(base []oaParameter, extra ...oaParameter) []oaParameter {
	return append(base, extra...)
}

// publicRoutes is every route the public API server serves.
func publicRoutes() []routeSpec {
	return []routeSpec{
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
			Scope:       "sessions:write",
			Request:     CreateSessionOptions{},
			Success:     201, Response: apitypes.Session{},
		},
		{
			Method: "GET", Path: "/api/v1/sessions", Tag: "Sessions",
			Summary: "List sessions",
			Scope:   "sessions:read",
			Query: withQuery(paginationQuery(),
				repeatedQuery("status", "Filter by session status."),
				stringQuery("agent", "Filter by agent type."),
				stringQuery("lifecycle_mode", "Filter by lifecycle mode."),
				labelQuery(),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.Session]{},
		},
		{
			Method: "GET", Path: "/api/v1/sessions/{sessionID}", Tag: "Sessions",
			Summary: "Get a session",
			Scope:   "sessions:read",
			Success: 200, Response: apitypes.Session{},
		},
		{
			Method: "DELETE", Path: "/api/v1/sessions/{sessionID}", Tag: "Sessions",
			Summary:     "Terminate a session",
			Description: "Ends the session and releases its resources. Terminated sessions cannot be resumed.",
			Scope:       "sessions:write",
			Success:     204,
		},
		{
			Method: "POST", Path: "/api/v1/sessions/{sessionID}/suspend", Tag: "Sessions",
			Summary:     "Suspend a session",
			Description: "Releases the runner while preserving the workspace and the agent context, so the session can be resumed later.",
			Scope:       "sessions:write",
			Success:     204,
		},
		{
			Method: "POST", Path: "/api/v1/sessions/{sessionID}/resume", Tag: "Sessions",
			Summary:     "Resume a suspended session",
			Description: "Acquires a runner, restores the workspace and context, and delivers any permission responses that arrived while the session was suspended.",
			Scope:       "sessions:write",
			Success:     204,
		},

		// ---- Tasks ----
		{
			Method: "POST", Path: "/api/v1/tasks", Tag: "Tasks",
			Summary:     "Create a task",
			Description: "Queues a prompt in a session. If the session is active the task is dispatched immediately; if it is pending or suspended, creating the task also brings it up.",
			Scope:       "tasks:write",
			Request:     CreateTaskOptions{},
			Success:     201, Response: apitypes.Task{},
		},
		{
			Method: "GET", Path: "/api/v1/tasks", Tag: "Tasks",
			Summary: "List tasks",
			Scope:   "tasks:read",
			Query: withQuery(paginationQuery(),
				stringQuery("session_id", "Filter by session."),
				repeatedQuery("status", "Filter by task status."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.Task]{},
		},
		{
			Method: "GET", Path: "/api/v1/tasks/{taskID}", Tag: "Tasks",
			Summary: "Get a task",
			Scope:   "tasks:read",
			Success: 200, Response: apitypes.Task{},
		},
		{
			Method: "POST", Path: "/api/v1/tasks/{taskID}/execute", Tag: "Tasks",
			Summary:     "Execute a pending task",
			Description: "Dispatches a task that is still pending. Returns as soon as the runner has accepted the work.",
			Scope:       "tasks:write",
			Success:     202, Response: apitypes.TaskExecutionAccepted{},
		},
		{
			Method: "POST", Path: "/api/v1/tasks/{taskID}/cancel", Tag: "Tasks",
			Summary: "Cancel a task",
			Scope:   "tasks:write",
			Success: 204,
		},
		{
			Method: "POST", Path: "/api/v1/tasks/{taskID}/retry", Tag: "Tasks",
			Summary:     "Retry a failed task",
			Description: "Starts a new run of a failed task. Fails with 409 once max_retries is reached.",
			Scope:       "tasks:write",
			Success:     204,
		},
		{
			Method: "GET", Path: "/api/v1/tasks/{taskID}/runs", Tag: "Tasks",
			Summary:     "List task runs",
			Description: "Lists the execution attempts of a task, oldest attempt first. The task itself only carries the latest status, so this is where a retry that failed before a later attempt succeeded is visible.",
			Scope:       "tasks:read",
			Query: withQuery(paginationQuery(),
				repeatedQuery("status", "Filter by run status."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.TaskRun]{},
		},
		{
			Method: "GET", Path: "/api/v1/tasks/{taskID}/logs", Tag: "Tasks",
			Summary: "List task logs",
			Scope:   "tasks:read",
			Query: withQuery(paginationQuery(),
				repeatedQuery("level", "Filter by log level."),
				repeatedQuery("stream", "Filter by output stream."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.Log]{},
		},

		// ---- Runners ----
		{
			Method: "GET", Path: "/api/v1/runners", Tag: "Runners",
			Summary:     "List runners",
			Description: "Runners are managed by the server; this view is read-only. Provisioning lives in the admin API.",
			Scope:       "runners:read",
			Query: withQuery(paginationQuery(),
				repeatedQuery("status", "Filter by runner status."),
				stringQuery("pool_name", "Filter by pool."),
				labelQuery(),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.Runner]{},
		},
		{
			Method: "GET", Path: "/api/v1/runners/{runnerID}", Tag: "Runners",
			Summary: "Get a runner",
			Scope:   "runners:read",
			Success: 200, Response: apitypes.Runner{},
		},

		// ---- Permissions ----
		{
			Method: "GET", Path: "/api/v1/permissions", Tag: "Permissions",
			Summary:     "List permission requests",
			Description: "Permission requests never expire on their own: they stay pending until approved, denied, or cancelled with the task.",
			Scope:       "permissions:read",
			Query: withQuery(paginationQuery(),
				stringQuery("session_id", "Filter by session."),
				stringQuery("task_id", "Filter by task."),
				repeatedQuery("status", "Filter by request status."),
				repeatedQuery("risk_level", "Filter by risk level."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.PermissionRequest]{},
		},
		{
			Method: "GET", Path: "/api/v1/permissions/{permissionID}", Tag: "Permissions",
			Summary: "Get a permission request",
			Scope:   "permissions:read",
			Success: 200, Response: apitypes.PermissionRequest{},
		},
		{
			Method: "POST", Path: "/api/v1/permissions/{permissionID}/approve", Tag: "Permissions",
			Summary:     "Approve a permission request",
			Description: "May be called while the session is suspended; the response is delivered when the session resumes.",
			Scope:       "permissions:write",
			Request:     ApproveOptions{},
			Success:     204,
		},
		{
			Method: "POST", Path: "/api/v1/permissions/{permissionID}/deny", Tag: "Permissions",
			Summary: "Deny a permission request",
			Scope:   "permissions:write",
			Request: DenyOptions{},
			Success: 204,
		},

		// ---- Workspaces ----
		{
			Method: "POST", Path: "/api/v1/workspaces", Tag: "Workspaces",
			Summary: "Create a workspace",
			Scope:   "workspaces:write",
			Request: CreateWorkspaceOptions{},
			Success: 201, Response: apitypes.Workspace{},
		},
		{
			Method: "GET", Path: "/api/v1/workspaces", Tag: "Workspaces",
			Summary: "List workspaces",
			Scope:   "workspaces:read",
			Query:   paginationQuery(),
			Success: 200, Response: apitypes.ListResponse[apitypes.Workspace]{},
		},
		{
			Method: "GET", Path: "/api/v1/workspaces/{workspaceID}", Tag: "Workspaces",
			Summary: "Get a workspace",
			Scope:   "workspaces:read",
			Success: 200, Response: apitypes.Workspace{},
		},
		{
			Method: "PATCH", Path: "/api/v1/workspaces/{workspaceID}", Tag: "Workspaces",
			Summary: "Update a workspace",
			Scope:   "workspaces:write",
			Request: UpdateWorkspaceOptions{},
			Success: 200, Response: apitypes.Workspace{},
		},
		{
			Method: "DELETE", Path: "/api/v1/workspaces/{workspaceID}", Tag: "Workspaces",
			Summary:     "Delete a workspace",
			Description: "Soft-deletes the workspace. Fails with 409 while a session still uses it.",
			Scope:       "workspaces:write",
			Success:     204,
		},

		// ---- Scheduled tasks ----
		{
			Method: "POST", Path: "/api/v1/scheduled-tasks", Tag: "Scheduled Tasks",
			Summary: "Create a scheduled task",
			Scope:   "scheduled-tasks:write",
			Request: CreateScheduledTaskOptions{},
			Success: 201, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "GET", Path: "/api/v1/scheduled-tasks", Tag: "Scheduled Tasks",
			Summary: "List scheduled tasks",
			Scope:   "scheduled-tasks:read",
			Query: withQuery(paginationQuery(),
				stringQuery("session_id", "Filter by session."),
				repeatedQuery("status", "Filter by scheduled task status."),
			),
			Success: 200, Response: apitypes.ListResponse[apitypes.ScheduledTask]{},
		},
		{
			Method: "GET", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}", Tag: "Scheduled Tasks",
			Summary: "Get a scheduled task",
			Scope:   "scheduled-tasks:read",
			Success: 200, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "PATCH", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}", Tag: "Scheduled Tasks",
			Summary: "Update a scheduled task",
			Scope:   "scheduled-tasks:write",
			Request: UpdateScheduledTaskOptions{},
			Success: 200, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "DELETE", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}", Tag: "Scheduled Tasks",
			Summary: "Delete a scheduled task",
			Scope:   "scheduled-tasks:write",
			Success: 204,
		},
		{
			Method: "POST", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}/pause", Tag: "Scheduled Tasks",
			Summary: "Pause a scheduled task",
			Scope:   "scheduled-tasks:write",
			Success: 200, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "POST", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}/resume", Tag: "Scheduled Tasks",
			Summary: "Resume a paused scheduled task",
			Scope:   "scheduled-tasks:write",
			Success: 200, Response: apitypes.ScheduledTask{},
		},
		{
			Method: "POST", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}/trigger", Tag: "Scheduled Tasks",
			Summary:     "Trigger a scheduled task now",
			Description: "Creates the task the schedule would have created, without waiting for the next occurrence.",
			Scope:       "scheduled-tasks:write",
			Success:     200, Response: apitypes.Task{},
		},

		// ---- Tunnels ----
		{
			Method: "POST", Path: "/api/v1/sessions/{sessionID}/tunnels", Tag: "Tunnels",
			Summary:     "Open a tunnel into a session",
			Description: "Forwards a port inside the runner through the API server. The response is the only place the tunnel token is readable.",
			Scope:       "tunnels:write",
			Request:     CreateTunnelOptions{},
			Success:     201, Response: apitypes.Tunnel{},
		},
		{
			Method: "GET", Path: "/api/v1/sessions/{sessionID}/tunnels", Tag: "Tunnels",
			Summary: "List a session's tunnels",
			Scope:   "tunnels:read",
			Success: 200, Response: apitypes.ListResponse[apitypes.Tunnel]{},
		},
		{
			Method: "GET", Path: "/api/v1/tunnels/{tunnelID}", Tag: "Tunnels",
			Summary: "Get a tunnel",
			Scope:   "tunnels:read",
			Success: 200, Response: apitypes.Tunnel{},
		},
		{
			Method: "DELETE", Path: "/api/v1/tunnels/{tunnelID}", Tag: "Tunnels",
			Summary: "Close a tunnel",
			Scope:   "tunnels:write",
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
			Scope:       "tasks:read",
			Query:       []oaParameter{stringQuery("token", "API key, for clients that cannot set an Authorization header.")},
			Success:     101,
		},
		{
			Method: "GET", Path: "/api/v1/events", Tag: "Streaming",
			Summary:     "Stream server events over WebSocket",
			Description: "Emits permission requests and session, task and runner state changes.",
			Scope:       "events:read",
			Query: []oaParameter{
				repeatedQuery("event_type", "Only deliver these event types."),
				stringQuery("labels", "JSON object of labels to filter on."),
				stringQuery("token", "API key, for clients that cannot set an Authorization header."),
			},
			Success: 101,
		},
		{
			Method: "GET", Path: "/api/v1/streams/{streamID}/ws", Tag: "Streaming",
			Summary:     "Stream browser frames over WebSocket",
			Description: "Sends rendered frames to the client and accepts input events back.",
			Scope:       "streams:read",
			Query:       []oaParameter{stringQuery("token", "API key, for clients that cannot set an Authorization header.")},
			Success:     101,
		},
	}
}
