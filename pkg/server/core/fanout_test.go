package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// fakeNotifier is a LISTEN/NOTIFY transport in memory.
//
// It loops published notifications straight back into the open stream, which is
// what Postgres does: a session hears its own process's notifications. That
// makes self-suppression testable here rather than only against a database.
type fakeNotifier struct {
	mu        sync.Mutex
	published []store.Notification
	stream    *fakeStream
	listens   int
	listenErr error
}

func (n *fakeNotifier) Notify(_ context.Context, channel, payload string) error {
	n.mu.Lock()
	n.published = append(n.published, store.Notification{Channel: channel, Payload: payload})
	stream := n.stream
	n.mu.Unlock()

	if stream != nil {
		stream.push(store.Notification{Channel: channel, Payload: payload})
	}
	return nil
}

func (n *fakeNotifier) Listen(_ context.Context, _ ...string) (store.NotificationStream, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.listens++
	if n.listenErr != nil {
		return nil, n.listenErr
	}

	n.stream = newFakeStream()
	return n.stream, nil
}

func (n *fakeNotifier) listenCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.listens
}

func (n *fakeNotifier) publishedCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.published)
}

// current returns the stream the relay is reading, waiting for it to exist.
func (n *fakeNotifier) current(t *testing.T) *fakeStream {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		stream := n.stream
		n.mu.Unlock()
		if stream != nil {
			return stream
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the relay never opened a listener")
	return nil
}

type fakeStream struct {
	ch   chan store.Notification
	fail chan struct{}

	mu     sync.Mutex
	closed bool
}

func newFakeStream() *fakeStream {
	return &fakeStream{ch: make(chan store.Notification, 64), fail: make(chan struct{})}
}

func (s *fakeStream) push(n store.Notification) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}

	select {
	case s.ch <- n:
	default:
	}
}

// breakStream makes the current connection fail, the way a dropped TCP session
// or a listener the server disconnected would.
func (s *fakeStream) breakStream() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.fail:
	default:
		close(s.fail)
	}
}

func (s *fakeStream) Next(ctx context.Context) (store.Notification, error) {
	select {
	case <-ctx.Done():
		return store.Notification{}, ctx.Err()
	case <-s.fail:
		return store.Notification{}, errors.New("connection reset by peer")
	case n := <-s.ch:
		return n, nil
	}
}

func (s *fakeStream) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// fanoutFixture is one replica's relay plus the managers it delivers into.
type fanoutFixture struct {
	relay    *LiveFanout
	notifier *fakeNotifier
	store    *testStore
	logs     *LogSubscriberManager
	events   *EventBus
	arrived  chan string
}

func newFanoutFixture(t *testing.T, replicaID string) *fanoutFixture {
	t.Helper()

	notifier := &fakeNotifier{}
	s := newTestStore()
	logs := NewLogSubscriberManager(zap.NewNop())
	events := NewEventBus(zap.NewNop())

	relay, err := NewLiveFanout(LiveFanoutConfig{
		Notifier:  notifier,
		Store:     s,
		Logs:      logs,
		Events:    events,
		ReplicaID: replicaID,
		Logger:    zap.NewNop(),
	})
	require.NoError(t, err)

	arrived := make(chan string, 64)
	relay.delivered = func(channel string) {
		select {
		case arrived <- channel:
		default:
		}
	}

	logs.setRelay(relay)
	events.setRelay(relay)

	require.NoError(t, relay.Start(context.Background()))
	t.Cleanup(func() { relay.Stop(context.Background()) })

	return &fanoutFixture{
		relay:    relay,
		notifier: notifier,
		store:    s,
		logs:     logs,
		events:   events,
		arrived:  arrived,
	}
}

func (f *fanoutFixture) inject(t *testing.T, channel string, notice any) {
	t.Helper()

	payload, err := json.Marshal(notice)
	require.NoError(t, err)
	f.notifier.current(t).push(store.Notification{Channel: channel, Payload: string(payload)})
}

func seedLogs(s *testStore, sessionID, runID string, from, to int64) {
	logs := make([]*store.Log, 0, to-from+1)
	for seq := from; seq <= to; seq++ {
		logs = append(logs, &store.Log{
			ID:        fmt.Sprintf("log_%s_%d", runID, seq),
			SessionID: sessionID,
			TaskID:    "task_" + runID,
			RunID:     runID,
			Sequence:  seq,
			Content:   "line",
		})
	}
	_ = s.CreateLogs(context.Background(), logs)
}

// The regression: a follow client on a replica that does not hold the runner's
// stream saw history and no tail at all.
func TestFanoutDeliversAPeersLogTail(t *testing.T) {
	fixture := newFanoutFixture(t, "replica-a")
	seedLogs(fixture.store, "sess_1", "run_1", 1, 3)

	sub := make(chan *store.Log, 16)
	fixture.logs.Subscribe("sess_1", sub)

	fixture.inject(t, FanoutLogChannel, logNotice{
		Origin: "replica-b", SessionID: "sess_1", RunID: "run_1", From: 1, To: 3,
	})

	var got []int64
	for i := 0; i < 3; i++ {
		select {
		case log := <-sub:
			got = append(got, log.Sequence)
		case <-time.After(5 * time.Second):
			t.Fatal("the peer's logs never arrived")
		}
	}
	assert.Equal(t, []int64{1, 2, 3}, got, "a tail delivered out of order is worse than no tail")
}

// The publishing replica has already delivered to its own subscribers, and
// Postgres hands it back its own notification. Without suppression every line
// would arrive twice on the replica that produced it.
func TestFanoutDoesNotDeliverItsOwnLogsTwice(t *testing.T) {
	fixture := newFanoutFixture(t, "replica-a")

	sub := make(chan *store.Log, 16)
	fixture.logs.Subscribe("sess_1", sub)

	batch := []*store.Log{
		{SessionID: "sess_1", RunID: "run_1", Sequence: 1, Content: "one"},
		{SessionID: "sess_1", RunID: "run_1", Sequence: 2, Content: "two"},
	}
	_ = fixture.store.CreateLogs(context.Background(), batch)
	fixture.logs.BroadcastBatch(batch)

	// Local delivery: exactly the batch.
	for i := 0; i < 2; i++ {
		select {
		case <-sub:
		case <-time.After(5 * time.Second):
			t.Fatal("local delivery is missing")
		}
	}

	// The loopback notification is now in flight. A peer's notice behind it is
	// the fence: once it has been delivered, the loopback has been processed.
	seedLogs(fixture.store, "sess_2", "run_2", 9, 9)
	other := make(chan *store.Log, 4)
	fixture.logs.Subscribe("sess_2", other)
	fixture.inject(t, FanoutLogChannel, logNotice{
		Origin: "replica-b", SessionID: "sess_2", RunID: "run_2", From: 9, To: 9,
	})
	select {
	case <-other:
	case <-time.After(5 * time.Second):
		t.Fatal("the fence notice never arrived")
	}

	select {
	case log := <-sub:
		t.Fatalf("the relay echoed this replica's own log back to it: seq %d", log.Sequence)
	default:
	}
}

// A replica nobody is following the session on must not query the database:
// that is what makes leaving the relay always on affordable.
func TestFanoutSkipsSessionsWithNoSubscribers(t *testing.T) {
	fixture := newFanoutFixture(t, "replica-a")
	seedLogs(fixture.store, "sess_quiet", "run_quiet", 1, 2)
	seedLogs(fixture.store, "sess_watched", "run_watched", 1, 1)

	watched := make(chan *store.Log, 4)
	fixture.logs.Subscribe("sess_watched", watched)

	fixture.inject(t, FanoutLogChannel, logNotice{
		Origin: "replica-b", SessionID: "sess_quiet", RunID: "run_quiet", From: 1, To: 2,
	})
	fixture.inject(t, FanoutLogChannel, logNotice{
		Origin: "replica-b", SessionID: "sess_watched", RunID: "run_watched", From: 1, To: 1,
	})

	select {
	case <-watched:
	case <-time.After(5 * time.Second):
		t.Fatal("the watched session's log never arrived")
	}
	assert.Equal(t, 1, fixture.store.logReadCount(),
		"only the session with a subscriber may cost a read")
}

// One notice per (session, run) group, not per line: a notification per log
// line would be a database round trip per line.
func TestFanoutPublishesOneNoticePerRun(t *testing.T) {
	fixture := newFanoutFixture(t, "replica-a")

	fixture.relay.PublishLogs([]*store.Log{
		{SessionID: "sess_1", RunID: "run_1", Sequence: 4},
		{SessionID: "sess_1", RunID: "run_1", Sequence: 1},
		{SessionID: "sess_1", RunID: "run_1", Sequence: 7},
		{SessionID: "sess_1", RunID: "run_2", Sequence: 2},
	})

	deadline := time.Now().Add(5 * time.Second)
	for fixture.notifier.publishedCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	fixture.notifier.mu.Lock()
	published := append([]store.Notification(nil), fixture.notifier.published...)
	fixture.notifier.mu.Unlock()

	require.Len(t, published, 2, "one notice per run, not one per line")

	var first logNotice
	require.NoError(t, json.Unmarshal([]byte(published[0].Payload), &first))
	assert.Equal(t, "run_1", first.RunID)
	assert.Equal(t, int64(1), first.From, "the range must cover the batch however it was ordered")
	assert.Equal(t, int64(7), first.To)
	assert.Equal(t, "replica-a", first.Origin)
}

func TestFanoutDeliversAPeersEvents(t *testing.T) {
	fixture := newFanoutFixture(t, "replica-a")

	sub, unsubscribe := fixture.events.Subscribe(nil)
	defer unsubscribe()

	fixture.inject(t, FanoutEventChannel, eventNotice{
		Origin:       "replica-b",
		Type:         "task.completed",
		ResourceID:   "task_123",
		ResourceType: "task",
		Timestamp:    time.Now(),
		Data:         json.RawMessage(`{"status":"completed"}`),
	})

	select {
	case evt := <-sub:
		assert.Equal(t, "task.completed", evt.Type)
		assert.Equal(t, "task_123", evt.ResourceID)
		raw, err := json.Marshal(evt.Data)
		require.NoError(t, err)
		assert.JSONEq(t, `{"status":"completed"}`, string(raw))
	case <-time.After(5 * time.Second):
		t.Fatal("the peer's event never arrived")
	}
}

func TestFanoutDoesNotDeliverItsOwnEventsTwice(t *testing.T) {
	fixture := newFanoutFixture(t, "replica-a")

	sub, unsubscribe := fixture.events.Subscribe(nil)
	defer unsubscribe()

	fixture.events.Publish(Event{Type: "task.created", ResourceID: "task_1", ResourceType: "task"})

	select {
	case evt := <-sub:
		assert.Equal(t, "task.created", evt.Type)
	case <-time.After(5 * time.Second):
		t.Fatal("local delivery is missing")
	}

	// Fence: a peer's event published behind the loopback.
	fixture.inject(t, FanoutEventChannel, eventNotice{
		Origin: "replica-b", Type: "task.completed", ResourceID: "task_2", ResourceType: "task",
	})
	select {
	case evt := <-sub:
		require.Equal(t, "task.completed", evt.Type,
			"the relay echoed this replica's own event back to it")
	case <-time.After(5 * time.Second):
		t.Fatal("the fence event never arrived")
	}
}

// An event is not a row anywhere, so an oversized payload cannot be re-read.
// It crosses without its data rather than not crossing at all.
func TestFanoutShedsAnOversizedEventPayload(t *testing.T) {
	fixture := newFanoutFixture(t, "replica-a")

	fixture.relay.PublishEvent(Event{
		Type:         "task.completed",
		ResourceID:   "task_big",
		ResourceType: "task",
		Data:         map[string]string{"output": strings.Repeat("x", store.MaxNotifyPayload)},
	})

	deadline := time.Now().Add(5 * time.Second)
	for fixture.notifier.publishedCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	fixture.notifier.mu.Lock()
	published := append([]store.Notification(nil), fixture.notifier.published...)
	fixture.notifier.mu.Unlock()
	require.Len(t, published, 1)
	assert.LessOrEqual(t, len(published[0].Payload), store.MaxNotifyPayload,
		"an oversized notice would be refused at NOTIFY time, losing the event entirely")

	var notice eventNotice
	require.NoError(t, json.Unmarshal([]byte(published[0].Payload), &notice))
	assert.True(t, notice.DataOmitted, "the receiver has to be able to tell data was dropped")
	assert.Equal(t, "task_big", notice.ResourceID)
}

// A listener connection dies for reasons nobody controls. What matters is that
// the relay comes back and keeps delivering; the notifications sent while it
// was down are gone, and that is the deal LISTEN/NOTIFY makes.
func TestFanoutReconnectsAfterTheListenerDrops(t *testing.T) {
	fixture := newFanoutFixture(t, "replica-a")
	seedLogs(fixture.store, "sess_1", "run_1", 1, 1)

	sub := make(chan *store.Log, 8)
	fixture.logs.Subscribe("sess_1", sub)

	first := fixture.notifier.current(t)
	require.Equal(t, 1, fixture.notifier.listenCount())
	first.breakStream()

	deadline := time.Now().Add(5 * time.Second)
	for fixture.notifier.listenCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	require.GreaterOrEqual(t, fixture.notifier.listenCount(), 2, "the relay never reconnected")

	fixture.inject(t, FanoutLogChannel, logNotice{
		Origin: "replica-b", SessionID: "sess_1", RunID: "run_1", From: 1, To: 1,
	})
	select {
	case log := <-sub:
		assert.Equal(t, int64(1), log.Sequence)
	case <-time.After(5 * time.Second):
		t.Fatal("the reconnected listener delivers nothing")
	}
}

// Publishing must never block the log ingest path on the database, so a full
// queue drops rather than waits. Dropping is visible.
func TestFanoutDropsRatherThanBlockingTheIngestPath(t *testing.T) {
	relay, err := NewLiveFanout(LiveFanoutConfig{
		Notifier:  &fakeNotifier{},
		Store:     newTestStore(),
		Logs:      NewLogSubscriberManager(zap.NewNop()),
		Events:    NewEventBus(zap.NewNop()),
		ReplicaID: "replica-a",
		Logger:    zap.NewNop(),
	})
	require.NoError(t, err)

	// Never started: nothing drains the queue, which is the same shape as a
	// database too slow to keep up.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < fanoutQueueDepth*2; i++ {
			relay.PublishLogs([]*store.Log{{SessionID: "s", RunID: "r", Sequence: int64(i)}})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publishing blocked when the queue was full")
	}
	assert.Positive(t, relay.Drops(), "a dropped notice must be counted, not silent")
}

func TestNewLiveFanoutRequiresANotifier(t *testing.T) {
	_, err := NewLiveFanout(LiveFanoutConfig{Logger: zap.NewNop()})
	require.Error(t, err)
}

// Wire builds the relay from a store that can listen, and leaves it nil for one
// that cannot - a store with no LISTEN/NOTIFY must not stop the server.
func TestWireBuildsTheRelayOnlyWhenTheStoreCanListen(t *testing.T) {
	plain := testWireDeps(t)
	app, err := Wire(plain)
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	assert.Nil(t, app.LiveFanout, "a store that cannot listen leaves the relay unwired")

	listening := testWireDeps(t)
	listening.Store = &listeningStore{testStore: newTestStore()}
	listening.ReplicaID = "repl_test"
	app, err = Wire(listening)
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	require.NotNil(t, app.LiveFanout)
	assert.Equal(t, "repl_test", app.LiveFanout.replicaID,
		"the relay and the routing registry must name this process the same way")

	disabled := testWireDeps(t)
	disabled.Store = &listeningStore{testStore: newTestStore()}
	disabled.Jobs.DisableLiveFanout = true
	app, err = Wire(disabled)
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	assert.Nil(t, app.LiveFanout)
}

// listeningStore is a testStore that also speaks LISTEN/NOTIFY.
type listeningStore struct {
	*testStore
	notifier fakeNotifier
}

func (s *listeningStore) Notify(ctx context.Context, channel, payload string) error {
	return s.notifier.Notify(ctx, channel, payload)
}

func (s *listeningStore) Listen(ctx context.Context, channels ...string) (store.NotificationStream, error) {
	return s.notifier.Listen(ctx, channels...)
}
