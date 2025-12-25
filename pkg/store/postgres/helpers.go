// Package postgres implements the store.Store interface using PostgreSQL.
//
//nolint:unused // Helper functions and constants used by CRUD operations in subsequent files
package postgres

import (
	"errors"
	"strings"

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
