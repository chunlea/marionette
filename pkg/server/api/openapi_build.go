package api

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

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

var pathParamPattern = regexp.MustCompile(`\{([^}]+)\}`)

type oaInfo struct {
	Title       string `yaml:"title"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
}

type oaServer struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
}

type oaTag struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type oaMediaType struct {
	Schema *oaSchema `yaml:"schema,omitempty"`
}

type oaRequestBody struct {
	Description string                 `yaml:"description,omitempty"`
	Required    bool                   `yaml:"required,omitempty"`
	Content     map[string]oaMediaType `yaml:"content"`
}

type oaResponse struct {
	Description string                 `yaml:"description"`
	Content     map[string]oaMediaType `yaml:"content,omitempty"`
}

type oaOperation struct {
	Tags        []string              `yaml:"tags"`
	Summary     string                `yaml:"summary"`
	Description string                `yaml:"description,omitempty"`
	OperationID string                `yaml:"operationId"`
	Security    []map[string][]string `yaml:"security,omitempty"`
	Parameters  []oaParameter         `yaml:"parameters,omitempty"`
	RequestBody *oaRequestBody        `yaml:"requestBody,omitempty"`
	Responses   *orderedMap           `yaml:"responses"`
}

type oaComponents struct {
	SecuritySchemes *orderedMap `yaml:"securitySchemes"`
	Schemas         *orderedMap `yaml:"schemas"`
}

type oaDocument struct {
	OpenAPI    string       `yaml:"openapi"`
	Info       oaInfo       `yaml:"info"`
	Servers    []oaServer   `yaml:"servers"`
	Tags       []oaTag      `yaml:"tags"`
	Paths      *orderedMap  `yaml:"paths"`
	Components oaComponents `yaml:"components"`
}

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
func BuildOpenAPIDocument() ([]byte, error) {
	registry := newSchemaRegistry()

	doc := oaDocument{
		OpenAPI: "3.1.0",
		Info: oaInfo{
			Title:       "Marionette API",
			Version:     SpecVersion,
			Description: apiDescription,
		},
		Servers: []oaServer{
			{URL: "http://localhost:8080", Description: "Local development"},
		},
		Tags: []oaTag{
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
		Paths: newOrderedMap(),
	}

	// The error body is shared by every failure response, so register it up
	// front rather than letting the first route that fails to reach it decide.
	registry.Schema(apitypes.ErrorResponse{})

	for _, route := range publicRoutes() {
		operation, err := buildOperation(registry, route)
		if err != nil {
			return nil, err
		}

		entry, ok := doc.Paths.Get(route.Path)
		if !ok {
			entry = newOrderedMap()
			doc.Paths.Set(route.Path, entry)
		}
		pathItem, isMap := entry.(*orderedMap)
		if !isMap {
			return nil, fmt.Errorf("openapi: path %q holds an unexpected value", route.Path)
		}
		method := strings.ToLower(route.Method)
		if _, duplicate := pathItem.Get(method); duplicate {
			return nil, fmt.Errorf("openapi: %s %s is declared twice", route.Method, route.Path)
		}
		pathItem.Set(method, operation)
	}
	doc.Paths.SortKeys()

	doc.Components = oaComponents{
		SecuritySchemes: newOrderedMap().Set("ApiKeyAuth", map[string]string{
			"type":         "http",
			"scheme":       "bearer",
			"description":  "An API key minted by the admin API.",
			"bearerFormat": "mk_...",
		}),
		Schemas: registry.Schemas(),
	}

	var buf bytes.Buffer
	buf.WriteString(specHeader)
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("openapi: encode document: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("openapi: close encoder: %w", err)
	}
	return buf.Bytes(), nil
}

func buildOperation(registry *schemaRegistry, route routeSpec) (*oaOperation, error) {
	operation := &oaOperation{
		Tags:        []string{route.Tag},
		Summary:     route.Summary,
		Description: route.Description,
		OperationID: operationID(route),
		Responses:   newOrderedMap(),
	}

	if route.Scope != "" {
		operation.Security = []map[string][]string{{"ApiKeyAuth": {route.Scope}}}
	}

	for _, name := range pathParamPattern.FindAllStringSubmatch(route.Path, -1) {
		operation.Parameters = append(operation.Parameters, oaParameter{
			Name:     name[1],
			In:       "path",
			Required: true,
			Schema:   &oaSchema{Type: "string"},
		})
	}
	operation.Parameters = append(operation.Parameters, route.Query...)

	if route.Request != nil {
		operation.RequestBody = &oaRequestBody{
			Required: true,
			Content: map[string]oaMediaType{
				"application/json": {Schema: registry.Schema(route.Request)},
			},
		}
	}

	contentType := route.ResponseContentType
	if contentType == "" {
		contentType = "application/json"
	}

	success := oaResponse{Description: route.SuccessDescription}
	if success.Description == "" {
		success.Description = defaultStatusDescription(route.Success)
	}
	if route.Response != nil {
		success.Content = map[string]oaMediaType{
			contentType: {Schema: registry.Schema(route.Response)},
		}
	} else if route.ResponseContentType != "" {
		success.Content = map[string]oaMediaType{contentType: {}}
	}
	operation.Responses.Set(strconv.Itoa(route.Success), success)

	errorContent := map[string]oaMediaType{
		"application/json": {Schema: ref("ErrorResponse")},
	}
	if route.Scope != "" {
		operation.Responses.Set("401", oaResponse{
			Description: "The API key is missing, malformed, revoked or expired.",
			Content:     errorContent,
		})
		operation.Responses.Set("403", oaResponse{
			Description: "The API key lacks the " + route.Scope + " scope.",
			Content:     errorContent,
		})
	}
	if strings.Contains(route.Path, "{") {
		operation.Responses.Set("404", oaResponse{
			Description: "No such resource.",
			Content:     errorContent,
		})
	}
	operation.Responses.Set("default", oaResponse{
		Description: "An error occurred.",
		Content:     errorContent,
	})

	return operation, nil
}

func defaultStatusDescription(status int) string {
	switch status {
	case 101:
		return "The connection was upgraded to a WebSocket."
	case 200:
		return "Success."
	case 201:
		return "Created."
	case 202:
		return "Accepted; the work continues asynchronously."
	case 204:
		return "Success, with no response body."
	default:
		return "Success."
	}
}

// operationID builds a stable, unique identifier from the method and path,
// e.g. GET /api/v1/sessions/{sessionID}/tunnels -> getSessionsTunnels.
//
// A path parameter contributes nothing except when it is the last segment,
// where it is the only thing distinguishing an item route from its collection:
// GET /sessions -> getSessions, GET /sessions/{sessionID} ->
// getSessionsBySessionID. Routes whose derivations still collide set
// OperationID explicitly; TestOperationIDsAreUniqueAndReadable enforces that.
func operationID(route routeSpec) string {
	if route.OperationID != "" {
		return route.OperationID
	}

	var segments []string
	for _, segment := range strings.Split(route.Path, "/") {
		// api and v1 are on every path and add nothing to the name.
		if segment == "" || segment == "api" || segment == "v1" {
			continue
		}
		segments = append(segments, segment)
	}

	id := strings.ToLower(route.Method)
	for i, segment := range segments {
		param := strings.HasPrefix(segment, "{")
		switch {
		case !param:
			id += camel(segment)
		case i == len(segments)-1:
			id += "By" + camel(strings.Trim(segment, "{}"))
		}
	}
	return id
}

// camel converts a path segment such as "scheduled-tasks" or "openapi.yaml"
// into "ScheduledTasks" / "OpenapiYaml".
func camel(segment string) string {
	var out strings.Builder
	upperNext := true
	for _, r := range segment {
		if r == '-' || r == '_' || r == '.' {
			upperNext = true
			continue
		}
		if upperNext {
			out.WriteString(strings.ToUpper(string(r)))
			upperNext = false
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
