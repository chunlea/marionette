// Package postgres implements the store.Store interface using PostgreSQL.
//
//nolint:unused // Helper functions and constants used by CRUD operations in subsequent files
package postgres

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/chunlea/marionette/pkg/store"
)

// PostgreSQL error codes.
const (
	pgErrUniqueViolation     = "23505"
	pgErrForeignKeyViolation = "23503"
	pgErrCheckViolation      = "23514"
)

// handlePgError converts PostgreSQL errors to store errors.
func handlePgError(err error, resource, identifier string) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch pgErr.Code {
	case pgErrUniqueViolation:
		field := extractFieldFromConstraint(pgErr.ConstraintName)
		return &store.AlreadyExistsError{
			Resource: resource,
			Field:    field,
			Value:    identifier,
		}
	case pgErrForeignKeyViolation:
		field := extractFieldFromConstraint(pgErr.ConstraintName)
		ref := extractReferenceFromConstraint(pgErr.ConstraintName)
		return &store.ForeignKeyError{
			Resource:    resource,
			Field:       field,
			Reference:   ref,
			ReferenceID: identifier,
		}
	case pgErrCheckViolation:
		return &store.InvalidInputError{
			Field:   pgErr.ColumnName,
			Message: pgErr.Message,
		}
	default:
		return err
	}
}

// extractFieldFromConstraint extracts the field name from a constraint name.
// Handles patterns like: idx_{table}_{field}_unique, {table}_{field}_key
func extractFieldFromConstraint(constraint string) string {
	parts := strings.Split(constraint, "_")
	if len(parts) >= 3 {
		// Try to get the second-to-last part (field name)
		return parts[len(parts)-2]
	}
	if len(parts) >= 2 {
		return parts[1]
	}
	return "unknown"
}

// extractReferenceFromConstraint extracts the referenced table from a constraint name.
// Handles patterns like: {table}_{field}_fkey
func extractReferenceFromConstraint(constraint string) string {
	if strings.HasSuffix(constraint, "_fkey") {
		parts := strings.Split(strings.TrimSuffix(constraint, "_fkey"), "_")
		if len(parts) >= 2 {
			// The field name often contains the reference table name
			return parts[len(parts)-1]
		}
	}
	parts := strings.Split(constraint, "_")
	if len(parts) >= 1 {
		return parts[0]
	}
	return "unknown"
}

// defaultLimit returns the limit to use, applying defaults and maximums.
func defaultLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

// emptyJSONObject returns an empty JSON object if the input is nil.
func emptyJSONObject(data []byte) []byte {
	if data == nil {
		return []byte("{}")
	}
	return data
}

// emptyJSONArray returns an empty JSON array if the input is nil.
func emptyJSONArray(data []byte) []byte {
	if data == nil {
		return []byte("[]")
	}
	return data
}

// ptrString returns a pointer to the string, or nil if empty.
func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// derefString returns the string value or empty string if nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefInt returns the int value or 0 if nil.
func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// encodeCursorValue builds an opaque pagination cursor from a sort-key value
// and the row ID. The ID is part of the cursor because the sort key alone is
// not unique: without it a page boundary that falls inside a run of equal keys
// either skips or repeats rows.
func encodeCursorValue(value, id string) string {
	return base64.URLEncoding.EncodeToString([]byte(value + "|" + id))
}

// decodeCursorValue splits a cursor back into its sort-key value and row ID.
func decodeCursorValue(cursor string) (value, id string, err error) {
	if cursor == "" {
		return "", "", nil
	}

	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", fmt.Errorf("invalid cursor encoding: %w", err)
	}

	parts := strings.SplitN(string(data), "|", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid cursor format")
	}

	return parts[0], parts[1], nil
}

// encodeCursor creates a cursor from timestamp and ID for pagination.
// Format: base64(RFC3339Nano|id)
func encodeCursor(t time.Time, id string) string {
	return encodeCursorValue(t.Format(time.RFC3339Nano), id)
}

// decodeCursor extracts timestamp and ID from a cursor.
// Returns zero values on error.
func decodeCursor(cursor string) (time.Time, string, error) {
	value, id, err := decodeCursorValue(cursor)
	if err != nil {
		return time.Time{}, "", err
	}
	if value == "" && id == "" {
		return time.Time{}, "", nil
	}

	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor timestamp: %w", err)
	}

	return t, id, nil
}
