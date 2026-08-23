package storemock_test

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/store/storemock"
)

// The tests below double as the usage examples other packages should copy.

func TestMockStoreRecordsCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := storemock.NewMockStore(ctrl)

	want := &store.Session{ID: "sess_1", Agent: "claude"}
	s.EXPECT().GetSession(gomock.Any(), "sess_1").Return(want, nil)

	got, err := s.GetSession(context.Background(), "sess_1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got != want {
		t.Errorf("GetSession() = %v, want %v", got, want)
	}
}

func TestMockStoreReturnsStoreErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := storemock.NewMockStore(ctrl)

	s.EXPECT().
		GetSession(gomock.Any(), "sess_missing").
		Return(nil, &store.NotFoundError{Resource: "session", ID: "sess_missing"})

	_, err := s.GetSession(context.Background(), "sess_missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSession() error = %v, want ErrNotFound", err)
	}
}

// TestMockStoreDrivesWithTx is the case the hand-written fakes made awkward:
// store.WithTx needs a Store that hands back a Tx, and a Tx is 133 methods.
//
// Each subtest builds its own controller: expectations live on the controller,
// so sharing one lets a permissive expectation from an earlier subtest absorb a
// later subtest's call.
func TestMockStoreDrivesWithTx(t *testing.T) {
	t.Run("commits when fn succeeds", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		ctx := context.Background()

		tx := storemock.NewMockTx(ctrl)
		s := storemock.NewMockStore(ctrl)

		s.EXPECT().BeginTx(gomock.Any()).Return(tx, nil)
		tx.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(nil)
		tx.EXPECT().Commit(gomock.Any()).Return(nil)
		// No Rollback expectation: WithTx defers one but skips it after a
		// successful commit, and an unexpected call fails the test.

		err := store.WithTx(ctx, s, func(tx store.Tx) error {
			return tx.CreateTask(ctx, &store.Task{ID: "task_1"})
		})
		if err != nil {
			t.Fatalf("WithTx() error = %v", err)
		}
	})

	t.Run("rolls back when fn fails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		ctx := context.Background()
		sentinel := errors.New("nope")

		tx := storemock.NewMockTx(ctrl)
		s := storemock.NewMockStore(ctrl)

		s.EXPECT().BeginTx(gomock.Any()).Return(tx, nil)
		tx.EXPECT().Rollback(gomock.Any()).Return(nil)

		err := store.WithTx(ctx, s, func(store.Tx) error { return sentinel })
		if !errors.Is(err, sentinel) {
			t.Fatalf("WithTx() error = %v, want %v", err, sentinel)
		}
	})
}
