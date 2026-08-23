package store

import (
	"context"
	"errors"
	"testing"
)

// fakeTx satisfies Tx by embedding the interface: only the transaction-control
// methods used by WithTx are implemented, everything else panics if called.
type fakeTx struct {
	Tx

	commitErr   error
	rollbackErr error
	commits     int
	rollbacks   int
}

func (f *fakeTx) Commit(context.Context) error {
	f.commits++
	return f.commitErr
}

func (f *fakeTx) Rollback(context.Context) error {
	f.rollbacks++
	return f.rollbackErr
}

// fakeStore satisfies Store the same way, only implementing BeginTx.
type fakeStore struct {
	Store

	tx       *fakeTx
	beginErr error
}

func (f *fakeStore) BeginTx(context.Context) (Tx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.tx, nil
}

func TestWithTx(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("boom")

	t.Run("commits when fn succeeds", func(t *testing.T) {
		tx := &fakeTx{}
		s := &fakeStore{tx: tx}

		called := false
		err := WithTx(ctx, s, func(got Tx) error {
			called = true
			if got != Tx(tx) {
				t.Fatalf("fn received a different transaction")
			}
			return nil
		})

		if err != nil {
			t.Fatalf("WithTx() = %v, want nil", err)
		}
		if !called {
			t.Fatal("fn was not called")
		}
		if tx.commits != 1 {
			t.Errorf("commits = %d, want 1", tx.commits)
		}
		if tx.rollbacks != 0 {
			t.Errorf("rollbacks = %d, want 0", tx.rollbacks)
		}
	})

	t.Run("rolls back and propagates fn error unwrapped", func(t *testing.T) {
		tx := &fakeTx{}
		s := &fakeStore{tx: tx}

		err := WithTx(ctx, s, func(Tx) error { return sentinel })

		if !errors.Is(err, sentinel) {
			t.Fatalf("WithTx() = %v, want %v", err, sentinel)
		}
		if tx.commits != 0 {
			t.Errorf("commits = %d, want 0", tx.commits)
		}
		if tx.rollbacks != 1 {
			t.Errorf("rollbacks = %d, want 1", tx.rollbacks)
		}
	})

	t.Run("store sentinels stay matchable", func(t *testing.T) {
		tx := &fakeTx{}
		s := &fakeStore{tx: tx}

		err := WithTx(ctx, s, func(Tx) error {
			return &NotFoundError{Resource: "session", ID: "sess_1"}
		})

		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("WithTx() = %v, want ErrNotFound", err)
		}
	})

	t.Run("wraps begin error", func(t *testing.T) {
		s := &fakeStore{beginErr: sentinel}

		err := WithTx(ctx, s, func(Tx) error {
			t.Fatal("fn must not be called when BeginTx fails")
			return nil
		})

		if !errors.Is(err, sentinel) {
			t.Fatalf("WithTx() = %v, want to wrap %v", err, sentinel)
		}
		if err.Error() == sentinel.Error() {
			t.Error("begin error should be wrapped with context")
		}
	})

	t.Run("wraps commit error", func(t *testing.T) {
		tx := &fakeTx{commitErr: sentinel}
		s := &fakeStore{tx: tx}

		err := WithTx(ctx, s, func(Tx) error { return nil })

		if !errors.Is(err, sentinel) {
			t.Fatalf("WithTx() = %v, want to wrap %v", err, sentinel)
		}
		if err.Error() == sentinel.Error() {
			t.Error("commit error should be wrapped with context")
		}
	})

	t.Run("joins rollback error with fn error", func(t *testing.T) {
		rbErr := errors.New("rollback failed")
		tx := &fakeTx{rollbackErr: rbErr}
		s := &fakeStore{tx: tx}

		err := WithTx(ctx, s, func(Tx) error { return sentinel })

		if !errors.Is(err, sentinel) {
			t.Errorf("WithTx() = %v, want to contain %v", err, sentinel)
		}
		if !errors.Is(err, rbErr) {
			t.Errorf("WithTx() = %v, want to contain %v", err, rbErr)
		}
	})

	t.Run("rolls back on panic and re-panics", func(t *testing.T) {
		tx := &fakeTx{}
		s := &fakeStore{tx: tx}

		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("panic did not propagate")
			}
			if tx.rollbacks != 1 {
				t.Errorf("rollbacks = %d, want 1", tx.rollbacks)
			}
			if tx.commits != 0 {
				t.Errorf("commits = %d, want 0", tx.commits)
			}
		}()

		_ = WithTx(ctx, s, func(Tx) error { panic("kaboom") })
	})
}
