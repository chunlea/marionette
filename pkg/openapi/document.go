package openapi

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var pathParamPattern = regexp.MustCompile(`\{([^}]+)\}`)

// Info is the document's info block.
type Info struct {
	Title       string `yaml:"title"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
}

// Server is one entry in the document's servers list.
type Server struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
}

// Tag groups operations in the rendered documentation.
type Tag struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Parameter is a path or query parameter.
type Parameter struct {
	Name        string  `yaml:"name"`
	In          string  `yaml:"in"`
	Description string  `yaml:"description,omitempty"`
	Required    bool    `yaml:"required,omitempty"`
	Explode     *bool   `yaml:"explode,omitempty"`
	Style       string  `yaml:"style,omitempty"`
	Schema      *Schema `yaml:"schema"`
}

// SecurityScheme describes how a caller authenticates.
type SecurityScheme struct {
	// Name is the key the scheme is registered under, and the one each
	// secured operation references.
	Name string
	// Fields are the scheme's properties, e.g. type/scheme/description.
	Fields map[string]string
}

type mediaType struct {
	Schema *Schema `yaml:"schema,omitempty"`
}

type requestBody struct {
	Description string               `yaml:"description,omitempty"`
	Required    bool                 `yaml:"required,omitempty"`
	Content     map[string]mediaType `yaml:"content"`
}

type response struct {
	Description string               `yaml:"description"`
	Content     map[string]mediaType `yaml:"content,omitempty"`
}

type operation struct {
	Tags        []string              `yaml:"tags"`
	Summary     string                `yaml:"summary"`
	Description string                `yaml:"description,omitempty"`
	OperationID string                `yaml:"operationId"`
	Security    []map[string][]string `yaml:"security,omitempty"`
	Parameters  []Parameter           `yaml:"parameters,omitempty"`
	RequestBody *requestBody          `yaml:"requestBody,omitempty"`
	Responses   *OrderedMap           `yaml:"responses"`
}

type components struct {
	SecuritySchemes *OrderedMap `yaml:"securitySchemes"`
	Schemas         *OrderedMap `yaml:"schemas"`
}

type document struct {
	OpenAPI    string      `yaml:"openapi"`
	Info       Info        `yaml:"info"`
	Servers    []Server    `yaml:"servers"`
	Tags       []Tag       `yaml:"tags"`
	Paths      *OrderedMap `yaml:"paths"`
	Components components  `yaml:"components"`
}

// Route describes one operation.
//
// The fields reflection cannot supply are all here: what the operation is for,
// what it answers with, what it reads from the query string, and what it takes
// to be allowed to call it.
type Route struct {
	Method string
	// Path uses OpenAPI templating, which is also chi's: /things/{thingID}.
	Path string
	Tag  string
	// OperationID overrides the identifier derived from the method and path.
	// Only needed where two routes would derive the same one.
	OperationID string
	Summary     string
	Description string
	// Secured marks the route as requiring the document's security scheme.
	Secured bool
	// Scopes are the scopes the route requires, for schemes that have them.
	Scopes []string
	// Query lists the query parameters the handler actually reads.
	Query []Parameter
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

// Spec is everything needed to render a document.
type Spec struct {
	Title       string
	Version     string
	Description string
	Servers     []Server
	Tags        []Tag
	Security    SecurityScheme
	Routes      []Route
	// ErrorSchema is a zero value of the type every failure response carries.
	ErrorSchema any
	// Header is prepended verbatim to the YAML, for the do-not-edit banner.
	Header string
}

// Build renders spec as an OpenAPI 3.1 document.
//
// The output is deterministic: paths and schemas are sorted, and everything
// else is emitted in declaration order, so a checked-in artifact can be diffed
// by CI without flapping.
func Build(spec Spec) ([]byte, error) {
	registry := NewSchemaRegistry()

	doc := document{
		OpenAPI: "3.1.0",
		Info: Info{
			Title:       spec.Title,
			Version:     spec.Version,
			Description: spec.Description,
		},
		Servers: spec.Servers,
		Tags:    spec.Tags,
		Paths:   NewOrderedMap(),
	}

	// The error body is shared by every failure response, so register it up
	// front rather than letting the first route that reaches it decide where
	// it lands in the file.
	errorSchemaRef := Ref("ErrorResponse")
	if spec.ErrorSchema != nil {
		errorSchemaRef = registry.For(spec.ErrorSchema)
	}

	for _, route := range spec.Routes {
		op, err := buildOperation(registry, spec, route, errorSchemaRef)
		if err != nil {
			return nil, err
		}

		entry, ok := doc.Paths.Get(route.Path)
		if !ok {
			entry = NewOrderedMap()
			doc.Paths.Set(route.Path, entry)
		}
		pathItem, isMap := entry.(*OrderedMap)
		if !isMap {
			return nil, fmt.Errorf("openapi: path %q holds an unexpected value", route.Path)
		}
		method := strings.ToLower(route.Method)
		if _, duplicate := pathItem.Get(method); duplicate {
			return nil, fmt.Errorf("openapi: %s %s is declared twice", route.Method, route.Path)
		}
		pathItem.Set(method, op)
	}
	doc.Paths.SortKeys()

	schemes := NewOrderedMap()
	if spec.Security.Name != "" {
		schemes.Set(spec.Security.Name, spec.Security.Fields)
	}
	doc.Components = components{
		SecuritySchemes: schemes,
		Schemas:         registry.Schemas(),
	}

	var buf bytes.Buffer
	buf.WriteString(spec.Header)
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

func buildOperation(registry *SchemaRegistry, spec Spec, route Route, errorSchema *Schema) (*operation, error) {
	op := &operation{
		Tags:        []string{route.Tag},
		Summary:     route.Summary,
		Description: route.Description,
		OperationID: OperationID(route),
		Responses:   NewOrderedMap(),
	}

	if route.Secured {
		scopes := route.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		op.Security = []map[string][]string{{spec.Security.Name: scopes}}
	}

	for _, name := range pathParamPattern.FindAllStringSubmatch(route.Path, -1) {
		op.Parameters = append(op.Parameters, Parameter{
			Name:     name[1],
			In:       "path",
			Required: true,
			Schema:   &Schema{Type: "string"},
		})
	}
	op.Parameters = append(op.Parameters, route.Query...)

	if route.Request != nil {
		op.RequestBody = &requestBody{
			Required: true,
			Content: map[string]mediaType{
				"application/json": {Schema: registry.For(route.Request)},
			},
		}
	}

	contentType := route.ResponseContentType
	if contentType == "" {
		contentType = "application/json"
	}

	success := response{Description: route.SuccessDescription}
	if success.Description == "" {
		success.Description = defaultStatusDescription(route.Success)
	}
	if route.Response != nil {
		success.Content = map[string]mediaType{
			contentType: {Schema: registry.For(route.Response)},
		}
	} else if route.ResponseContentType != "" {
		success.Content = map[string]mediaType{contentType: {}}
	}
	op.Responses.Set(strconv.Itoa(route.Success), success)

	errorContent := map[string]mediaType{"application/json": {Schema: errorSchema}}
	if route.Secured {
		op.Responses.Set("401", response{
			Description: spec.Security.UnauthorizedDescription(),
			Content:     errorContent,
		})
	}
	if len(route.Scopes) > 0 {
		op.Responses.Set("403", response{
			Description: "The credential lacks the " + strings.Join(route.Scopes, ", ") + " scope.",
			Content:     errorContent,
		})
	}
	if strings.Contains(route.Path, "{") {
		op.Responses.Set("404", response{
			Description: "No such resource.",
			Content:     errorContent,
		})
	}
	op.Responses.Set("default", response{
		Description: "An error occurred.",
		Content:     errorContent,
	})

	return op, nil
}

// UnauthorizedDescription describes a 401 for this scheme.
func (s SecurityScheme) UnauthorizedDescription() string {
	if desc, ok := s.Fields["unauthorized"]; ok {
		return desc
	}
	return "The credential is missing, malformed, revoked or expired."
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

// OperationID builds a stable, unique identifier from the method and path,
// e.g. GET /api/v1/sessions/{sessionID}/tunnels -> getSessionsTunnels.
//
// A path parameter contributes nothing except when it is the last segment,
// where it is the only thing distinguishing an item route from its collection:
// GET /sessions -> getSessions, GET /sessions/{sessionID} ->
// getSessionsBySessionID. Routes whose derivations still collide set
// OperationID explicitly; a uniqueness test should enforce that.
func OperationID(route Route) string {
	if route.OperationID != "" {
		return route.OperationID
	}

	var segments []string
	for _, segment := range strings.Split(route.Path, "/") {
		// Version and namespace segments are on every path in a group and add
		// nothing to the name.
		if segment == "" || segment == "api" || segment == "v1" || segment == "admin" {
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

// NormalizePath reconciles chi's pattern syntax with OpenAPI path templating:
// chi keeps the trailing slash of a subrouter's root and spells a catch-all as
// `*`. Coverage tests compare routes through this.
func NormalizePath(route string) string {
	route = strings.ReplaceAll(route, "/*", "/{path}")
	if len(route) > 1 {
		route = strings.TrimSuffix(route, "/")
	}
	return route
}
