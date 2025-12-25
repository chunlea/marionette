// Package store provides the persistence interface and data models for Marionette.
package store

import (
	"errors"
	"fmt"
)

// Sentinel errors for common store operations.
var (
	// ErrNotFound indicates the requested resource does not exist.
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists indicates a resource with the same unique key exists.
	ErrAlreadyExists = errors.New("already exists")

	// ErrConflict indicates a concurrent modification conflict.
	ErrConflict = errors.New("conflict")

	// ErrInvalidInput indicates invalid input parameters.
	ErrInvalidInput = errors.New("invalid input")

	// ErrForeignKeyViolation indicates a foreign key constraint violation.
	ErrForeignKeyViolation = errors.New("foreign key violation")

	// ErrTxClosed indicates the transaction has already been committed or rolled back.
	ErrTxClosed = errors.New("transaction closed")
)

// NotFoundError provides details about which resource was not found.
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	if e.ID == "" {
		return fmt.Sprintf("%s not found", e.Resource)
	}
	return fmt.Sprintf("%s %q not found", e.Resource, e.ID)
}

func (e *NotFoundError) Unwrap() error {
	return ErrNotFound
}

// AlreadyExistsError provides details about the duplicate resource.
type AlreadyExistsError struct {
	Resource string
	Field    string
	Value    string
}

func (e *AlreadyExistsError) Error() string {
	return fmt.Sprintf("%s with %s %q already exists", e.Resource, e.Field, e.Value)
}

func (e *AlreadyExistsError) Unwrap() error {
	return ErrAlreadyExists
}

// ForeignKeyError provides details about the constraint violation.
type ForeignKeyError struct {
	Resource    string
	Field       string
	Reference   string
	ReferenceID string
}

func (e *ForeignKeyError) Error() string {
	return fmt.Sprintf("%s.%s references non-existent %s %q",
		e.Resource, e.Field, e.Reference, e.ReferenceID)
}

func (e *ForeignKeyError) Unwrap() error {
	return ErrForeignKeyViolation
}

// InvalidInputError provides details about invalid input.
type InvalidInputError struct {
	Field   string
	Message string
}

func (e *InvalidInputError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func (e *InvalidInputError) Unwrap() error {
	return ErrInvalidInput
}
