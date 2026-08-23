package api

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// This file turns Go types into OpenAPI schemas by reflection.
//
// Why reflection rather than swaggo annotations or a spec-first generator:
// the repository already had three hand-written specs that had drifted from
// each other and from the code, so the requirement is that the spec cannot be
// wrong, not that it be expressive. apitypes is the exact set of types the
// handlers serialize, so reflecting over it makes the schema section true by
// construction, and it needs no new dependency, no code generator in the build
// path, and no annotation comments that can rot next to the struct they
// describe. What reflection cannot do — summaries, status codes, query
// parameters, scopes — is declared explicitly in openapi_routes.go.

// orderedMap is a YAML mapping with a deterministic key order.
//
// The document is checked into the repository and diffed by CI, so map
// iteration order would make it flap. Keys are emitted in insertion order.
type orderedMap struct {
	keys   []string
	values map[string]any
}

func newOrderedMap() *orderedMap {
	return &orderedMap{values: map[string]any{}}
}

// Set inserts or replaces a key, preserving first-insertion order.
func (m *orderedMap) Set(key string, value any) *orderedMap {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
	return m
}

// Get returns the value stored under key, if any.
func (m *orderedMap) Get(key string) (any, bool) {
	v, ok := m.values[key]
	return v, ok
}

// SortKeys switches the map to alphabetical order, used for the sections
// (paths, schemas) where insertion order carries no meaning.
func (m *orderedMap) SortKeys() {
	sort.Strings(m.keys)
}

// Len reports how many keys the map holds.
func (m *orderedMap) Len() int { return len(m.keys) }

// MarshalYAML renders the map as an ordered YAML mapping node.
func (m *orderedMap) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, key := range m.keys {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		// Quote keys that YAML would otherwise reinterpret, such as the
		// numeric HTTP status codes used as response keys.
		if _, err := fmt.Sscanf(key, "%d", new(int)); err == nil {
			keyNode.Style = yaml.DoubleQuotedStyle
		}
		valueNode := &yaml.Node{}
		if err := valueNode.Encode(m.values[key]); err != nil {
			return nil, fmt.Errorf("encode %q: %w", key, err)
		}
		node.Content = append(node.Content, keyNode, valueNode)
	}
	return node, nil
}

// oaSchema is a JSON Schema subset sufficient for this API.
type oaSchema struct {
	Ref                  string      `yaml:"$ref,omitempty"`
	Description          string      `yaml:"description,omitempty"`
	Type                 string      `yaml:"type,omitempty"`
	Format               string      `yaml:"format,omitempty"`
	Enum                 []string    `yaml:"enum,omitempty"`
	Items                *oaSchema   `yaml:"items,omitempty"`
	Properties           *orderedMap `yaml:"properties,omitempty"`
	AdditionalProperties *oaSchema   `yaml:"additionalProperties,omitempty"`
	Required             []string    `yaml:"required,omitempty"`
}

// schemaRegistry collects the named schemas referenced by the document.
type schemaRegistry struct {
	schemas *orderedMap
}

func newSchemaRegistry() *schemaRegistry {
	return &schemaRegistry{schemas: newOrderedMap()}
}

var timeType = reflect.TypeOf(time.Time{})

// ref returns a schema that references a named component.
func ref(name string) *oaSchema {
	return &oaSchema{Ref: "#/components/schemas/" + name}
}

// Schema returns the schema for v, registering every named struct it reaches.
// Passing a nil-typed value is a programming error and panics, because the
// route table is compiled in and a missing type is not a runtime condition.
func (r *schemaRegistry) Schema(v any) *oaSchema {
	return r.schemaForType(reflect.TypeOf(v))
}

func (r *schemaRegistry) schemaForType(t reflect.Type) *oaSchema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t == timeType {
		return &oaSchema{Type: "string", Format: "date-time"}
	}

	switch t.Kind() {
	case reflect.String:
		return &oaSchema{Type: "string"}
	case reflect.Bool:
		return &oaSchema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return &oaSchema{Type: "integer", Format: "int32"}
	case reflect.Int64:
		return &oaSchema{Type: "integer", Format: "int64"}
	case reflect.Float32, reflect.Float64:
		return &oaSchema{Type: "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			// []byte is base64-encoded by encoding/json.
			return &oaSchema{Type: "string", Format: "byte"}
		}
		return &oaSchema{Type: "array", Items: r.schemaForType(t.Elem())}
	case reflect.Map:
		return &oaSchema{Type: "object", AdditionalProperties: r.schemaForType(t.Elem())}
	case reflect.Struct:
		return r.registerStruct(t)
	default:
		panic(fmt.Sprintf("openapi: unsupported kind %s for type %s", t.Kind(), t))
	}
}

// registerStruct adds t to the component list (once) and returns a reference.
func (r *schemaRegistry) registerStruct(t reflect.Type) *oaSchema {
	name := schemaName(t)
	if _, exists := r.schemas.Get(name); exists {
		return ref(name)
	}
	// Reserve the name before walking the fields so a self-referencing type
	// cannot recurse forever.
	r.schemas.Set(name, &oaSchema{Type: "object"})

	schema := &oaSchema{Type: "object", Properties: newOrderedMap()}
	r.collectFields(t, schema)
	if schema.Properties.Len() == 0 {
		schema.Properties = nil
	}
	r.schemas.Set(name, schema)
	return ref(name)
}

func (r *schemaRegistry) collectFields(t reflect.Type, schema *oaSchema) {
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			r.collectFields(field.Type, schema)
			continue
		}
		if !field.IsExported() {
			continue
		}

		name, opts, ok := jsonFieldName(field)
		if !ok {
			continue
		}

		fieldSchema := r.schemaForType(field.Type)
		if enum := field.Tag.Get("enum"); enum != "" {
			// The enum belongs to the field, not to the shared string schema.
			fieldSchema = &oaSchema{Type: fieldSchema.Type, Format: fieldSchema.Format, Enum: strings.Split(enum, ",")}
		}
		schema.Properties.Set(name, fieldSchema)

		// A field is required when it is always present in the JSON: not a
		// pointer, and not tagged omitempty.
		if field.Type.Kind() != reflect.Pointer && !opts.omitEmpty {
			schema.Required = append(schema.Required, name)
		}
	}
}

type jsonTagOptions struct {
	omitEmpty bool
}

// jsonFieldName resolves the wire name of a struct field, reporting false for
// fields encoding/json skips.
func jsonFieldName(field reflect.StructField) (string, jsonTagOptions, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", jsonTagOptions{}, false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = field.Name
	}
	opts := jsonTagOptions{}
	for _, part := range parts[1:] {
		if part == "omitempty" {
			opts.omitEmpty = true
		}
	}
	return name, opts, true
}

// schemaName derives the component name for a struct type.
//
// Two rewrites keep the document readable: the generic list envelope becomes
// "<Item>List", and request option structs lose their Go-flavoured "Options"
// suffix in favour of "Request".
func schemaName(t reflect.Type) string {
	name := t.Name()
	if name == "" {
		panic(fmt.Sprintf("openapi: anonymous struct %s cannot be named", t))
	}

	if item, ok := listEnvelopeItem(name); ok {
		return item + "List"
	}
	if strings.HasSuffix(name, "Options") {
		return strings.TrimSuffix(name, "Options") + "Request"
	}
	return name
}

// listEnvelopeItem extracts "Session" from the reflected name of
// apitypes.ListResponse[apitypes.Session].
func listEnvelopeItem(name string) (string, bool) {
	const prefix = "ListResponse["
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, "]") {
		return "", false
	}
	arg := name[len(prefix) : len(name)-1]
	if idx := strings.LastIndex(arg, "."); idx >= 0 {
		arg = arg[idx+1:]
	}
	if arg == "" {
		return "", false
	}
	return arg, true
}

// Schemas returns the registered components, alphabetically ordered.
func (r *schemaRegistry) Schemas() *orderedMap {
	r.schemas.SortKeys()
	return r.schemas
}
