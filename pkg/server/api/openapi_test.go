package api

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/chunlea/marionette/pkg/openapi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// TestOpenAPIDocumentIsUpToDate is the drift check. It runs in the ordinary
// test job, so a change to a DTO or the route table that is not regenerated
// fails CI without needing a separate workflow.
func TestOpenAPIDocumentIsUpToDate(t *testing.T) {
	generated, err := BuildOpenAPIDocument()
	require.NoError(t, err)

	committed, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)

	if string(committed) != string(generated) {
		t.Fatalf("pkg/server/api/openapi.yaml is out of date; run 'make openapi' and commit the result")
	}
}

// TestOpenAPIDocumentIsDeterministic guards the ordered-map plumbing: if the
// document depended on Go map iteration order, the drift check above would
// flap instead of failing honestly.
func TestOpenAPIDocumentIsDeterministic(t *testing.T) {
	first, err := BuildOpenAPIDocument()
	require.NoError(t, err)
	for range 5 {
		again, err := BuildOpenAPIDocument()
		require.NoError(t, err)
		require.Equal(t, string(first), string(again))
	}
}

// TestOpenAPICoversEveryRoute walks the router the server actually serves and
// fails if an endpoint is missing from the document, in either direction.
// Three hand-written specs drifted into documenting about a third of the API;
// this is what stops that recurring.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	// Routes are registered unconditionally; only the tunnel proxy is gated on
	// its handler being configured, so that is the only service the walk needs.
	srv := New(Config{}, zap.NewNop(), WithTunnelProxy(&TunnelProxyHandler{}))

	served := map[string]bool{}
	require.NoError(t, chi.Walk(srv.Router(),
		func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			served[routeKey(method, route)] = true
			return nil
		}))

	documented := map[string]bool{}
	for _, route := range publicRoutes() {
		documented[routeKey(route.Method, route.Path)] = true
	}

	for route := range served {
		assert.Contains(t, documented, route,
			"route is served but missing from the OpenAPI document; add it to publicRoutes()")
	}
	for route := range documented {
		assert.Contains(t, served, route,
			"route is documented but not served; remove it from publicRoutes()")
	}
}

// routeKey normalizes a method and pattern so a chi route and a documented
// one compare equal.
//
// Two reconciliations are needed. chi keeps the trailing slash of a
// subrouter's root and spells the catch-all as `*`. And the tunnel proxy is
// registered with chi's HandleFunc, which binds every method — CONNECT
// included, which OpenAPI cannot express at all; it is a transparent
// passthrough rather than an API surface, so the document describes it once
// and the walk collapses its methods onto that entry.
func routeKey(method, route string) string {
	route = strings.ReplaceAll(route, "/*", "/{path}")
	if len(route) > 1 {
		route = strings.TrimSuffix(route, "/")
	}
	if route == "/tunnels/{tunnelID}/{path}" {
		method = "GET"
	}
	return method + " " + route
}

func TestOpenAPIDocumentParsesAndDescribesTheContract(t *testing.T) {
	raw, err := BuildOpenAPIDocument()
	require.NoError(t, err)

	var doc struct {
		OpenAPI string                                 `yaml:"openapi"`
		Paths   map[string]map[string]yaml.Node        `yaml:"paths"`
		Comps   struct{ Schemas map[string]yaml.Node } `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	assert.Equal(t, "3.1.0", doc.OpenAPI)

	// The endpoints the review found missing from every previous spec.
	for _, path := range []string{
		"/api/v1/workspaces",
		"/api/v1/scheduled-tasks",
		"/api/v1/tasks/{taskID}/execute",
		"/api/v1/sessions/{sessionID}/tunnels",
		"/api/v1/logs/{taskID}/stream",
		"/api/v1/streams/{streamID}/ws",
		"/api/v1/events",
		// TaskRun had a type and a mapper but no route, so the dashboard faked
		// an empty run history. The endpoint now exists, and the document
		// describes exactly what is served.
		"/api/v1/tasks/{taskID}/runs",
	} {
		assert.Contains(t, doc.Paths, path)
	}

	for _, schema := range []string{
		"Session", "Task", "Runner", "SessionList", "ErrorResponse",
		"TaskRun", "TaskRunList",
	} {
		assert.Contains(t, doc.Comps.Schemas, schema)
	}
}

// TestOpenAPIDocumentHidesInternalFields is the schema-level counterpart of
// TestResponsesOmitInternalFields: the mappers may drop a column, but the
// published contract must not advertise it either.
func TestOpenAPIDocumentHidesInternalFields(t *testing.T) {
	raw, err := BuildOpenAPIDocument()
	require.NoError(t, err)

	for _, field := range internalFields {
		assert.NotContains(t, string(raw), "\n          "+field+":",
			"the OpenAPI document declares an internal field")
	}
}

func TestRepeatedQueryParametersDeclareRepeatKeyForm(t *testing.T) {
	// axios serializes arrays as status[]=a by default while Go reads
	// r.URL.Query()["status"]; the spec has to say which form is right so a
	// generated client gets it too.
	param := openapi.RepeatedQuery("status", "Filter by status.")
	require.NotNil(t, param.Explode)
	assert.True(t, *param.Explode)
	assert.Equal(t, "form", param.Style)
	assert.Equal(t, "array", param.Schema.Type)
}

func TestOperationIDsAreUniqueAndReadable(t *testing.T) {
	seen := map[string]string{}
	for _, route := range publicRoutes() {
		id := openapi.OperationID(route)
		if previous, duplicate := seen[id]; duplicate {
			t.Fatalf("operationId %q is used by both %s and %s %s", id, previous, route.Method, route.Path)
		}
		seen[id] = route.Method + " " + route.Path
	}

	assert.Equal(t, "getSessions", openapi.OperationID(openapi.Route{
		Method: "GET", Path: "/api/v1/sessions",
	}))
	assert.Equal(t, "getSessionsBySessionID", openapi.OperationID(openapi.Route{
		Method: "GET", Path: "/api/v1/sessions/{sessionID}",
	}))
	assert.Equal(t, "getSessionsTunnels", openapi.OperationID(openapi.Route{
		Method: "GET", Path: "/api/v1/sessions/{sessionID}/tunnels",
	}))
	assert.Equal(t, "postScheduledTasksTrigger", openapi.OperationID(openapi.Route{
		Method: "POST", Path: "/api/v1/scheduled-tasks/{scheduledTaskID}/trigger",
	}))
}

// Schema name rewriting moved to pkg/openapi with the generator; its own tests
// cover it. Duplicating them here would only assert that the import still
// resolves.
