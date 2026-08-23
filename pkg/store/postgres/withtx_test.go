package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

func txTestRunner(suffix string) *store.Runner {
	return &store.Runner{
		Name:         "withtx-" + suffix + "-" + time.Now().Format("150405.000000"),
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
}

func TestWithTx(t *testing.T) {
	ctx := context.Background()

	t.Run("commits on success", func(t *testing.T) {
		runner := txTestRunner("commit")

		err := store.WithTx(ctx, testStore, func(tx store.Tx) error {
			return tx.CreateRunner(ctx, runner)
		})
		require.NoError(t, err)

		got, err := testStore.GetRunner(ctx, runner.ID)
		require.NoError(t, err)
		assert.Equal(t, runner.Name, got.Name)

		_ = testStore.DeleteRunner(ctx, runner.ID)
	})

	t.Run("rolls back every write when fn fails", func(t *testing.T) {
		first := txTestRunner("rollback-1")
		second := txTestRunner("rollback-2")
		sentinel := errors.New("business rule violated")

		err := store.WithTx(ctx, testStore, func(tx store.Tx) error {
			if err := tx.CreateRunner(ctx, first); err != nil {
				return err
			}
			if err := tx.CreateRunner(ctx, second); err != nil {
				return err
			}
			return sentinel
		})
		require.ErrorIs(t, err, sentinel)

		_, err = testStore.GetRunner(ctx, first.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
		_, err = testStore.GetRunner(ctx, second.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("rolls back on panic", func(t *testing.T) {
		runner := txTestRunner("panic")

		func() {
			defer func() {
				assert.NotNil(t, recover(), "panic must propagate")
			}()
			_ = store.WithTx(ctx, testStore, func(tx store.Tx) error {
				require.NoError(t, tx.CreateRunner(ctx, runner))
				panic("kaboom")
			})
		}()

		_, err := testStore.GetRunner(ctx, runner.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("propagates store errors from inside the transaction", func(t *testing.T) {
		runner := txTestRunner("duplicate")
		require.NoError(t, testStore.CreateRunner(ctx, runner))
		defer func() { _ = testStore.DeleteRunner(ctx, runner.ID) }()

		duplicate := txTestRunner("duplicate-other")
		duplicate.Name = runner.Name

		err := store.WithTx(ctx, testStore, func(tx store.Tx) error {
			return tx.CreateRunner(ctx, duplicate)
		})
		assert.ErrorIs(t, err, store.ErrAlreadyExists)
	})
}
