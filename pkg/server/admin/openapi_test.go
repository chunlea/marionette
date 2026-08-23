package admin

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/chunlea/marionette/pkg/observability/health"
	"github.com/chunlea/marionette/pkg/openapi"
)

// TestAdminOpenAPIDocumentIsUpToDate is the drift check. It runs in the
// ordinary test job, so a change to a DTO or the route table that is not
// regenerated fails CI without needing a separate workflow.
func TestAdminOpenAPIDocumentIsUpToDate(t *testing.T) {
	generated, err := BuildOpenAPIDocument()
	require.NoError(t, err)

	committed, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)

	if string(committed) != string(generated) {
		t.Fatalf("pkg/server/admin/openapi.yaml is out of date; run 'make openapi' and commit the result")
	}
}

func TestAdminOpenAPIDocumentIsDeterministic(t *testing.T) {
	first, err := BuildOpenAPIDocument()
	require.NoError(t, err)
	for range 5 {
		again, err := BuildOpenAPIDocument()
		require.NoError(t, err)
		require.Equal(t, string(first), string(again))
	}
}

// coverageServer builds a router with every conditional handler present, so
// the walk sees the whole surface rather than the subset a given deployment
// happens to configure.
func coverageServer(t *testing.T) *Server {
	t.Helper()
	srv, err := New(Config{Username: "u", Password: "p"}, zap.NewNop(),
		WithHealthService(stubHealthService{}),
		WithStreamsHandler(&StreamsHandler{}),
		WithSignalingHandler(&SignalingHandler{}),
	)
	require.NoError(t, err)
	return srv
}

type stubHealthService struct{}

func (stubHealthService) CheckLiveness(_ context.Context) health.Response  { return health.Response{} }
func (stubHealthService) CheckReadiness(_ context.Context) health.Response { return health.Response{} }

// TestAdminOpenAPICoversEveryRoute walks the router the admin server actually
// serves and fails on any route missing from the document, in either
// direction. The previous hand-written document described 9 of more than
// fifty; this is what stops that recurring.
func TestAdminOpenAPICoversEveryRoute(t *testing.T) {
	served := map[string]bool{}
	require.NoError(t, chi.Walk(coverageServer(t).Router(),
		func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			served[routeKey(method, route)] = true
			return nil
		}))

	documented := map[string]bool{}
	for _, route := range adminRoutes() {
		documented[routeKey(route.Method, route.Path)] = true
	}

	for route := range served {
		assert.Contains(t, documented, route,
			"route is served but missing from the admin OpenAPI document; add it to adminRoutes()")
	}
	for route := range documented {
		assert.Contains(t, served, route,
			"route is documented but not served; remove it from adminRoutes()")
	}
}

// routeKey normalizes a method and pattern so a chi route and a documented one
// compare equal.
//
// The dashboard catch-all is registered with chi's Handle, which binds every
// method including CONNECT, a method OpenAPI cannot express. It is a static
// file server rather than an API surface, so the document describes it once
// and the walk collapses its methods onto that entry.
func routeKey(method, route string) string {
	route = openapi.NormalizePath(route)
	if route == "/{path}" {
		method = "GET"
	}
	return method + " " + route
}

func TestAdminDocumentDescribesTheWholeSurface(t *testing.T) {
	raw, err := BuildOpenAPIDocument()
	require.NoError(t, err)

	var doc struct {
		OpenAPI    string                          `yaml:"openapi"`
		Paths      map[string]map[string]yaml.Node `yaml:"paths"`
		Components struct {
			Schemas         map[string]yaml.Node `yaml:"schemas"`
			SecuritySchemes map[string]yaml.Node `yaml:"securitySchemes"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	assert.Equal(t, "3.1.0", doc.OpenAPI)

	// Everything the previous hand-written document left out.
	for _, path := range []string{
		"/admin/api/v1/profiles",
		"/admin/api/v1/runner-tokens",
		"/admin/api/v1/runner-tokens/{tokenID}/rotate",
		"/admin/api/v1/action-logs",
		"/admin/api/v1/webhooks",
		"/admin/api/v1/webhook-events",
		"/admin/api/v1/sessions/{sessionID}/activate",
		"/admin/api/v1/streams",
		"/admin/api/v1/signaling",
		"/api/status",
	} {
		assert.Contains(t, doc.Paths, path)
	}

	for _, schema := range []string{
		"APIKey", "CreatedAPIKey", "RunnerToken", "CreatedRunnerToken", "AgentConfig",
		"ProviderConfig", "Profile", "Runner", "Webhook", "WebhookEvent", "ActionLog",
		"ErrorResponse",
	} {
		assert.Contains(t, doc.Components.Schemas, schema)
	}

	// Basic auth, and only basic auth: the document used to advertise a bearer
	// mode the middleware has never implemented.
	assert.Contains(t, doc.Components.SecuritySchemes, "basicAuth")
	assert.Len(t, doc.Components.SecuritySchemes, 1)
	assert.NotContains(t, string(raw), "bearerFormat")
}

// TestAdminDocumentDeclaresNoSecret is the schema-level counterpart of
// TestAdminResponsesWithholdSecrets: the mappers may withhold a column, but
// the published contract must not advertise one either.
func TestAdminDocumentDeclaresNoSecret(t *testing.T) {
	raw, err := BuildOpenAPIDocument()
	require.NoError(t, err)

	for _, field := range secretFieldNames {
		assert.NotContains(t, string(raw), "\n          "+field+":",
			"the admin document declares an internal field")
	}
}

func TestAdminOperationIDsAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, route := range adminRoutes() {
		id := openapi.OperationID(route)
		if previous, duplicate := seen[id]; duplicate {
			t.Fatalf("operationId %q is used by both %s and %s %s", id, previous, route.Method, route.Path)
		}
		seen[id] = route.Method + " " + route.Path
	}
}

// Every route under /admin/api/v1 sits behind basic auth; nothing outside it
// does. A route table that disagreed with the middleware would document an
// endpoint as open when it is not, or the reverse.
func TestDocumentedSecurityMatchesTheMiddleware(t *testing.T) {
	for _, route := range adminRoutes() {
		underAuth := strings.HasPrefix(route.Path, "/admin/api/v1")
		assert.Equalf(t, underAuth, route.Secured,
			"%s %s: documented security does not match where the middleware is mounted",
			route.Method, route.Path)
	}
}
