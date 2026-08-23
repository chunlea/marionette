package api

import (
	"github.com/chunlea/marionette/pkg/openapi"
	"github.com/chunlea/marionette/pkg/server/api/apitypes"
)

// SpecVersion is the version reported by the generated OpenAPI document.
const SpecVersion = "1.0.0"

// specHeader is prepended to the generated file so nobody edits it by hand
// again. Three hand-written specs is how this API ended up with roughly forty
// undocumented endpoints.
const specHeader = `# GENERATED FILE — DO NOT EDIT.
#
# Source of truth: the route table in pkg/server/api/openapi_routes.go and the
# Go types in pkg/server/api/apitypes.
# Regenerate with:  make openapi
# Drift is checked by TestOpenAPIDocumentIsUpToDate, which runs in CI.
`

const apiDescription = `The public Marionette API: create sessions, run tasks on them, answer the
agent's permission requests, and stream the output.

Authenticate with an API key as ` + "`Authorization: Bearer mk_...`" + `. Each route
requires a scope, listed in its security requirement. The two WebSocket routes
also accept the key as a ` + "`?token=`" + ` query parameter, because a browser cannot
set headers on a WebSocket handshake.

List endpoints are cursor-paginated: pass ` + "`next_cursor`" + ` from one response as
` + "`cursor`" + ` on the next request, and stop when ` + "`has_more`" + ` is false. Repeated
filters such as ` + "`status`" + ` are sent as repeated keys (` + "`?status=a&status=b`" + `),
not as ` + "`status[]`" + `.`

// BuildOpenAPIDocument renders the OpenAPI description of the public API.
//
// It is the single source the served spec, the checked-in artifact and the
// generated TypeScript types all come from.

// BuildOpenAPIDocument renders the OpenAPI description of the public API.
//
// The rendering itself lives in pkg/openapi, which the admin document also uses.
// This file is now only the parts that are specific to this API: what it is
// called, what it is for, and how a caller authenticates.
func BuildOpenAPIDocument() ([]byte, error) {
	return openapi.Build(openapi.Spec{
		Title:       "Marionette API",
		Version:     SpecVersion,
		Description: apiDescription,
		Servers: []openapi.Server{
			{URL: "http://localhost:8080", Description: "Local development"},
		},
		Tags: []openapi.Tag{
			{Name: "Sessions", Description: "Long-lived work contexts that outlive individual runners."},
			{Name: "Tasks", Description: "Units of work executed inside a session."},
			{Name: "Runners", Description: "Execution environments, read-only from the public API."},
			{Name: "Permissions", Description: "Approval requests raised by the agent mid-task."},
			{Name: "Workspaces", Description: "Persistent working directories."},
			{Name: "Scheduled Tasks", Description: "Tasks created on a cron schedule."},
			{Name: "Tunnels", Description: "Ports forwarded out of a runner."},
			{Name: "Streaming", Description: "WebSocket endpoints for logs, events and frames."},
			{Name: "Service", Description: "Health and documentation endpoints."},
		},
		Security:    apiSecurity,
		Routes:      publicRoutes(),
		ErrorSchema: apitypes.ErrorResponse{},
		Header:      specHeader,
	})
}
