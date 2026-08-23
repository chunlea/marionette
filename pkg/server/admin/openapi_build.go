package admin

import (
	"github.com/chunlea/marionette/pkg/openapi"
	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
)

// SpecVersion is the version reported by the generated admin document.
const SpecVersion = "1.0.0"

const specHeader = `# GENERATED FILE — DO NOT EDIT.
#
# Source of truth: the route table in pkg/server/admin/openapi_routes.go and
# the Go types in pkg/server/admin/admintypes.
# Regenerate with:  make openapi
# Drift is checked by TestAdminOpenAPIDocumentIsUpToDate, which runs in CI.
`

const adminDescription = `The Marionette admin API: mint API keys, register runner tokens,
configure agents and providers, define runner profiles, read the audit trail, and manage
webhooks.

This API is the deployment's root of trust. A caller holding the admin credentials can mint
a key with any scope, register a runner that will be handed real work, and read every
session. Treat the port as an internal one: bind it to a private interface, and do not put
it behind the same ingress as the public API.

Authenticate with HTTP basic auth, using the credentials from ` + "`MARIONETTE_UI_USERNAME`" + `
and ` + "`MARIONETTE_UI_PASSWORD`" + `. The server refuses to start without them unless the
operator passes ` + "`--dev-insecure-admin`" + `, which serves this API with no authentication
at all and is for development only.

Secrets are readable exactly once. Creating an API key or a runner token returns
` + "`raw_token`" + `; creating or rotating a webhook returns ` + "`secret`" + `. No other
response carries a credential, and none can be read back later — only the prefix, which
identifies it.

List endpoints return ` + "`items`" + `, ` + "`total_count`" + ` and ` + "`has_more`" + `. Most
page by cursor: pass ` + "`next_cursor`" + ` as ` + "`cursor`" + `. The streams listing pages by
offset instead.`

// BuildOpenAPIDocument renders the OpenAPI description of the admin API.
func BuildOpenAPIDocument() ([]byte, error) {
	return openapi.Build(openapi.Spec{
		Title:       "Marionette Admin API",
		Version:     SpecVersion,
		Description: adminDescription,
		Servers: []openapi.Server{
			{URL: "http://localhost:8081", Description: "Local development"},
		},
		Tags: []openapi.Tag{
			{Name: "API Keys", Description: "Credentials for the public API."},
			{Name: "Runner Tokens", Description: "Credentials pool runners present when they join."},
			{Name: "Agent Configs", Description: "Per-agent credentials and settings."},
			{Name: "Provider Configs", Description: "Where runners come from."},
			{Name: "Profiles", Description: "Reusable runner shapes."},
			{Name: "Runners", Description: "Provisioning and teardown."},
			{Name: "Sessions", Description: "Operator overrides on session lifecycle."},
			{Name: "Action Logs", Description: "The audit trail."},
			{Name: "Webhooks", Description: "Event subscriptions."},
			{Name: "Webhook Events", Description: "Delivery attempts."},
			{Name: "Streaming", Description: "Desktop streaming; a frozen subsystem."},
			{Name: "Service", Description: "Health, status, documentation and the dashboard."},
		},
		Security:    adminSecurity,
		Routes:      adminRoutes(),
		ErrorSchema: admintypes.ErrorResponse{},
		Header:      specHeader,
	})
}
