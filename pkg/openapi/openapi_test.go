package openapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type sampleNested struct {
	Value string `json:"value"`
}

type sampleError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type sample struct {
	ID        string            `json:"id"`
	Status    string            `json:"status" enum:"a,b"`
	Optional  *string           `json:"optional,omitempty"`
	Skipped   string            `json:"-"`
	Count     int64             `json:"count"`
	Ratio     float64           `json:"ratio"`
	Flag      bool              `json:"flag"`
	Tags      []string          `json:"tags"`
	Labels    map[string]string `json:"labels"`
	Nested    *sampleNested     `json:"nested,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	//nolint:unused // present precisely to prove reflection skips it
	unexported  string
	OmitEmptied string `json:"omit_emptied,omitempty"`
}

type sampleList struct {
	Items []*sample `json:"items"`
}

func TestSchemaReflectsGoTypes(t *testing.T) {
	registry := NewSchemaRegistry()
	require.Equal(t, "#/components/schemas/sample", registry.For(sample{}).Ref)

	raw, err := yaml.Marshal(registry.Schemas())
	require.NoError(t, err)
	rendered := string(raw)

	assert.Contains(t, rendered, "created_at")
	assert.Contains(t, rendered, "format: date-time")
	assert.Contains(t, rendered, "- a")
	assert.Contains(t, rendered, "$ref: '#/components/schemas/sampleNested'")

	assert.NotContains(t, rendered, "Skipped", `a json:"-" field must not reach the document`)
	assert.NotContains(t, rendered, "unexported")
}

func TestRequiredIsWhatIsAlwaysPresent(t *testing.T) {
	registry := NewSchemaRegistry()
	registry.For(sample{})
	value, ok := registry.Schemas().Get("sample")
	require.True(t, ok)
	schema := value.(*Schema)

	// A field is required exactly when JSON always carries it: not a pointer,
	// and not tagged omitempty.
	assert.Contains(t, schema.Required, "id")
	assert.Contains(t, schema.Required, "count")
	assert.NotContains(t, schema.Required, "optional")
	assert.NotContains(t, schema.Required, "omit_emptied")
	assert.NotContains(t, schema.Required, "nested")
}

func TestListEnvelopesAreNamedAfterTheirItem(t *testing.T) {
	item, ok := listEnvelopeItem("ListResponse[github.com/x/y.Session]")
	require.True(t, ok)
	assert.Equal(t, "Session", item)

	_, ok = listEnvelopeItem("Session")
	assert.False(t, ok)
}

func TestOptionSuffixBecomesRequest(t *testing.T) {
	registry := NewSchemaRegistry()
	type CreateThingOptions struct {
		Name string `json:"name"`
	}
	assert.Equal(t, "#/components/schemas/CreateThingRequest", registry.For(CreateThingOptions{}).Ref)
}

func testSpec(routes ...Route) Spec {
	return Spec{
		Title:       "Test API",
		Version:     "1.0.0",
		Description: "A test.",
		Servers:     []Server{{URL: "http://localhost:1", Description: "local"}},
		Tags:        []Tag{{Name: "Things", Description: "Things."}},
		Security: SecurityScheme{
			Name:   "basicAuth",
			Fields: map[string]string{"type": "http", "scheme": "basic"},
		},
		Routes:      routes,
		ErrorSchema: sampleError{},
		Header:      "# generated\n",
	}
}

func TestBuildRendersAnOperation(t *testing.T) {
	raw, err := Build(testSpec(Route{
		Method: "GET", Path: "/things/{thingID}", Tag: "Things",
		Summary: "Get a thing", Secured: true,
		Success: 200, Response: sample{},
	}))
	require.NoError(t, err)

	var doc struct {
		OpenAPI string `yaml:"openapi"`
		Paths   map[string]map[string]struct {
			OperationID string                `yaml:"operationId"`
			Security    []map[string][]string `yaml:"security"`
			Responses   map[string]yaml.Node  `yaml:"responses"`
			Parameters  []Parameter           `yaml:"parameters"`
		} `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	assert.Equal(t, "3.1.0", doc.OpenAPI)
	op := doc.Paths["/things/{thingID}"]["get"]
	assert.Equal(t, "getThingsByThingID", op.OperationID)
	assert.Equal(t, []map[string][]string{{"basicAuth": {}}}, op.Security)

	// A secured route documents 401; one with a path parameter documents 404;
	// a scope-less scheme documents no 403.
	assert.Contains(t, op.Responses, "200")
	assert.Contains(t, op.Responses, "401")
	assert.Contains(t, op.Responses, "404")
	assert.NotContains(t, op.Responses, "403")
	assert.Contains(t, op.Responses, "default")

	require.Len(t, op.Parameters, 1)
	assert.Equal(t, "path", op.Parameters[0].In)
	assert.True(t, op.Parameters[0].Required)
}

func TestScopedRoutesDocumentForbidden(t *testing.T) {
	raw, err := Build(testSpec(Route{
		Method: "GET", Path: "/things", Tag: "Things",
		Summary: "List things", Secured: true, Scopes: []string{"things:read"},
		Success: 200, Response: sampleList{},
	}))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "things:read")
	assert.Contains(t, string(raw), `"403"`)
}

func TestUnsecuredRoutesDocumentNoAuthFailure(t *testing.T) {
	raw, err := Build(testSpec(Route{
		Method: "GET", Path: "/health", Tag: "Things",
		Summary: "Health", Success: 200, Response: sample{},
	}))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"401"`)
	assert.NotContains(t, string(raw), "security:")
}

func TestBuildIsDeterministic(t *testing.T) {
	spec := testSpec(
		Route{Method: "GET", Path: "/zebras", Tag: "Things", Summary: "z", Success: 200, Response: sample{}},
		Route{Method: "GET", Path: "/apples", Tag: "Things", Summary: "a", Success: 200, Response: sampleList{}},
		Route{Method: "POST", Path: "/apples", Tag: "Things", Summary: "b", Request: sample{}, Success: 201, Response: sample{}},
	)
	first, err := Build(spec)
	require.NoError(t, err)
	for range 5 {
		again, err := Build(spec)
		require.NoError(t, err)
		require.Equal(t, string(first), string(again))
	}
	// Paths are sorted so a reordered route table does not reshuffle the file.
	assert.Less(t, strings_Index(string(first), "/apples"), strings_Index(string(first), "/zebras"))
}

func TestBuildRejectsADuplicateOperation(t *testing.T) {
	_, err := Build(testSpec(
		Route{Method: "GET", Path: "/things", Tag: "Things", Summary: "one", Success: 200, Response: sample{}},
		Route{Method: "GET", Path: "/things", Tag: "Things", Summary: "two", Success: 200, Response: sample{}},
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declared twice")
}

func TestRepeatedQueryDeclaresRepeatKeyForm(t *testing.T) {
	param := RepeatedQuery("status", "Filter by status.")
	require.NotNil(t, param.Explode)
	assert.True(t, *param.Explode)
	assert.Equal(t, "form", param.Style)
	assert.Equal(t, "array", param.Schema.Type)
}

func TestNormalizePath(t *testing.T) {
	assert.Equal(t, "/admin/api/v1/keys", NormalizePath("/admin/api/v1/keys/"))
	assert.Equal(t, "/tunnels/{id}/{path}", NormalizePath("/tunnels/{id}/*"))
	assert.Equal(t, "/", NormalizePath("/"))
}

func TestOperationIDDropsNamespaceSegments(t *testing.T) {
	assert.Equal(t, "getKeys", OperationID(Route{Method: "GET", Path: "/admin/api/v1/keys"}))
	assert.Equal(t, "getKeysByKeyID", OperationID(Route{Method: "GET", Path: "/admin/api/v1/keys/{keyID}"}))
	assert.Equal(t, "postRunnerTokensRotate", OperationID(Route{
		Method: "POST", Path: "/admin/api/v1/runner-tokens/{tokenID}/rotate",
	}))
	assert.Equal(t, "explicit", OperationID(Route{Method: "GET", Path: "/x", OperationID: "explicit"}))
}

func TestOrderedMapPreservesInsertionOrder(t *testing.T) {
	m := NewOrderedMap().Set("zebra", 1).Set("apple", 2).Set("zebra", 3)
	raw, err := yaml.Marshal(m)
	require.NoError(t, err)
	assert.Equal(t, "zebra: 3\napple: 2\n", string(raw))
	assert.Equal(t, 2, m.Len())

	m.SortKeys()
	raw, err = yaml.Marshal(m)
	require.NoError(t, err)
	assert.Equal(t, "apple: 2\nzebra: 3\n", string(raw))
}

func TestOrderedMapQuotesNumericKeys(t *testing.T) {
	// Response codes are keys; unquoted, YAML would read 200 as an integer.
	raw, err := yaml.Marshal(NewOrderedMap().Set("200", "ok"))
	require.NoError(t, err)
	assert.Equal(t, `"200": ok`+"\n", string(raw))
}

func strings_Index(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
