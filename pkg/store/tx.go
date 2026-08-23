package store

import (
	"context"
	"errors"
	"fmt"
)

// WithTx runs fn inside a database transaction.
//
// The transaction is committed when fn returns nil and rolled back otherwise,
// including when fn panics or returns early. The error returned by fn is
// propagated unchanged so callers can still match the store sentinels with
// errors.Is; only Begin/Commit failures are wrapped.
//
// The Tx handed to fn must not be used after fn returns.
//
//	err := store.WithTx(ctx, s, func(tx store.Tx) error {
//	    if err := tx.UpdateSession(ctx, sessionID, updates); err != nil {
//	        return err
//	    }
//	    return tx.CreateTask(ctx, task)
//	})
func WithTx(ctx context.Context, s Store, fn func(tx Tx) error) (err error) {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	// Rollback is a no-op once the transaction has been committed, so a single
	// deferred call covers the fn-error path, an early return and a panic.
	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(ctx); rbErr != nil && err != nil {
			err = errors.Join(err, fmt.Errorf("rolling back transaction: %w", rbErr))
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	committed = true

	return nil
}
