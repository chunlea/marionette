package core

import (
	"testing"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestLogSubscriberManager_NewLogSubscriberManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	lsm := NewLogSubscriberManager(logger)
	require.NotNil(t, lsm)
	assert.Equal(t, 0, lsm.SubscriberCount("any_session"))
}

func TestLogSubscriberManager_Subscribe(t *testing.T) {
	logger := zaptest.NewLogger(t)
	lsm := NewLogSubscriberManager(logger)

	ch := make(chan *store.Log, 10)
	sessionID := "sess_test"

	// Subscribe
	lsm.Subscribe(sessionID, ch)
	assert.Equal(t, 1, lsm.SubscriberCount(sessionID))

	// Subscribe another channel
	ch2 := make(chan *store.Log, 10)
	lsm.Subscribe(sessionID, ch2)
	assert.Equal(t, 2, lsm.SubscriberCount(sessionID))

	// Different session
	ch3 := make(chan *store.Log, 10)
	lsm.Subscribe("sess_other", ch3)
	assert.Equal(t, 2, lsm.SubscriberCount(sessionID))
	assert.Equal(t, 1, lsm.SubscriberCount("sess_other"))
}

func TestLogSubscriberManager_Unsubscribe(t *testing.T) {
	logger := zaptest.NewLogger(t)
	lsm := NewLogSubscriberManager(logger)

	ch1 := make(chan *store.Log, 10)
	ch2 := make(chan *store.Log, 10)
	sessionID := "sess_test"

	lsm.Subscribe(sessionID, ch1)
	lsm.Subscribe(sessionID, ch2)
	assert.Equal(t, 2, lsm.SubscriberCount(sessionID))

	// Unsubscribe one
	lsm.Unsubscribe(sessionID, ch1)
	assert.Equal(t, 1, lsm.SubscriberCount(sessionID))

	// Unsubscribe the other
	lsm.Unsubscribe(sessionID, ch2)
	assert.Equal(t, 0, lsm.SubscriberCount(sessionID))

	// Unsubscribe non-existent (should not panic)
	lsm.Unsubscribe(sessionID, ch1)
	lsm.Unsubscribe("nonexistent", ch1)
}

func TestLogSubscriberManager_Broadcast(t *testing.T) {
	logger := zaptest.NewLogger(t)
	lsm := NewLogSubscriberManager(logger)

	sessionID := "sess_test"
	ch1 := make(chan *store.Log, 10)
	ch2 := make(chan *store.Log, 10)

	lsm.Subscribe(sessionID, ch1)
	lsm.Subscribe(sessionID, ch2)

	// Broadcast a log
	log := &store.Log{
		ID:        "log_test",
		SessionID: sessionID,
		TaskID:    "task_test",
		Content:   []byte("test message"),
		Sequence:  1,
	}
	lsm.Broadcast(log)

	// Both channels should receive the log
	received1 := <-ch1
	received2 := <-ch2
	assert.Equal(t, log, received1)
	assert.Equal(t, log, received2)
}

func TestLogSubscriberManager_Broadcast_NoSubscribers(t *testing.T) {
	logger := zaptest.NewLogger(t)
	lsm := NewLogSubscriberManager(logger)

	// Broadcast to session with no subscribers (should not panic)
	log := &store.Log{
		ID:        "log_test",
		SessionID: "sess_no_subscribers",
		Content:   []byte("test message"),
	}
	lsm.Broadcast(log) // Should complete without error
}

func TestLogSubscriberManager_Broadcast_FullChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	lsm := NewLogSubscriberManager(logger)

	sessionID := "sess_test"
	// Create a channel with buffer size 1
	ch := make(chan *store.Log, 1)
	lsm.Subscribe(sessionID, ch)

	// Fill the channel
	log1 := &store.Log{ID: "log_1", SessionID: sessionID, Content: []byte("first")}
	lsm.Broadcast(log1)

	// Second broadcast should be dropped (channel full)
	log2 := &store.Log{ID: "log_2", SessionID: sessionID, Content: []byte("second")}
	lsm.Broadcast(log2) // Should not block, message dropped

	// Verify first message is in channel
	received := <-ch
	assert.Equal(t, "log_1", received.ID)
}
