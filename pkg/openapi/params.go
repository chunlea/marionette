package openapi

// Query parameter constructors shared by both API surfaces, so the two cannot
// describe the same filter differently.

// explodeRepeatedKeys is the OpenAPI spelling of ?key=a&key=b.
var explodeRepeatedKeys = true

// RepeatedQuery describes a filter that may be given more than once, e.g.
// ?status=pending&status=running.
//
// Go reads these with r.URL.Query()[key], so the document has to say
// style=form + explode. An axios client left on its defaults sends
// status[]=pending instead, which Go does not see at all — every filter is
// then silently ignored, which is exactly what the dashboard used to do.
func RepeatedQuery(name, description string) Parameter {
	return Parameter{
		Name:        name,
		In:          "query",
		Description: description,
		Style:       "form",
		Explode:     &explodeRepeatedKeys,
		Schema:      &Schema{Type: "array", Items: &Schema{Type: "string"}},
	}
}

// CSVQuery describes a filter passed as one comma-separated value.
//
// Preferring RepeatedQuery is right for new endpoints; this exists because
// some handlers already split on commas and changing them would break callers.
func CSVQuery(name, description string) Parameter {
	return Parameter{
		Name:        name,
		In:          "query",
		Description: description,
		Schema:      &Schema{Type: "string"},
	}
}

// StringQuery describes a plain string filter.
func StringQuery(name, description string) Parameter {
	return Parameter{Name: name, In: "query", Description: description, Schema: &Schema{Type: "string"}}
}

// IntQuery describes an integer parameter.
func IntQuery(name, description string) Parameter {
	return Parameter{Name: name, In: "query", Description: description, Schema: &Schema{Type: "integer", Format: "int32"}}
}

// BoolQuery describes a boolean flag.
func BoolQuery(name, description string) Parameter {
	return Parameter{Name: name, In: "query", Description: description, Schema: &Schema{Type: "boolean"}}
}

// TimeQuery describes an RFC 3339 timestamp parameter.
func TimeQuery(name, description string) Parameter {
	return Parameter{Name: name, In: "query", Description: description, Schema: &Schema{Type: "string", Format: "date-time"}}
}

// PaginationQuery is the cursor pagination pair every list endpoint accepts.
func PaginationQuery() []Parameter {
	return []Parameter{
		IntQuery("limit", "Maximum number of items to return. Defaults to 50."),
		StringQuery("cursor", "Opaque cursor from a previous response's next_cursor."),
	}
}

// LabelQuery documents a label filter.
func LabelQuery(description string) Parameter {
	return Parameter{
		Name:        "labels",
		In:          "query",
		Description: description,
		Schema:      &Schema{Type: "string"},
	}
}

// WithQuery appends parameters to a base set, for composing PaginationQuery
// with an endpoint's own filters.
func WithQuery(base []Parameter, extra ...Parameter) []Parameter {
	return append(base, extra...)
}
