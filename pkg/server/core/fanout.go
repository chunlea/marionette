package core

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Live fan-out: logs and events reach subscribers on every replica.
//
// Logs arrive on the runner's control stream, which terminates in exactly one
// process, and events are published by whichever process performed the action.
// Both were then broadcast to in-memory subscribers of that one process. A
// `tasks logs --follow` client or an SSE consumer connected to any other
// replica saw history from the database and no live tail at all - and in the
// shipped three-replica production overlay that is most of them.
//
// The transport is Postgres LISTEN/NOTIFY, because of what is being carried: a
// dropped notification costs a gap in a tail, and the rows it points at are in
// the database either way. That is a tolerable loss in a way a dropped command
// is not, which is why cross-replica *commands* got a registry and one
// point-to-point hop (R5) and this got a doorbell.
//
// Three properties make it safe to leave always on rather than gating it on how
// many replicas are live:
//
//   - publishing is one small statement per log BATCH (not per line) and per
//     event, and it happens on a background goroutine, so the ingest path pays
//     a channel send and never waits for the database;
//   - a replica with no subscriber for the session drops the notification
//     without touching the database at all;
//   - the listener is one connection per process, idle when nothing is
//     happening.
//
// A gate keyed on the live replica count would save that one statement and buy
// a race in exchange: a replica that has just joined would miss the tail until
// every publisher's count refreshed. Not worth it.
const (
	// FanoutLogChannel carries pointers to freshly written log batches.
	FanoutLogChannel = "log_events"

	// FanoutEventChannel carries resource lifecycle events.
	FanoutEventChannel = "bus_events"
)

const (
	// fanoutQueueDepth bounds how many notices wait to be published.
	//
	// The queue exists so the log ingest path never blocks on the database. It
	// is deliberately shallow: a deep queue would trade the same latency for
	// memory and deliver a tail so far behind that nothing would want it.
	fanoutQueueDepth = 256

	// fanoutLogReadLimit bounds one read-back of a notified batch. A batch
	// larger than this loses the tail beyond it; the rows are still in the
	// database, and history reads them.
	fanoutLogReadLimit = 500

	// fanoutReadTimeout bounds the read-back of a notified batch.
	fanoutReadTimeout = 5 * time.Second

	// fanoutMinBackoff and fanoutMaxBackoff bound listener reconnection.
	// A LISTEN gap is survivable by design, so reconnecting is not urgent - but
	// it is the whole subsystem, so it is not slow either.
	fanoutMinBackoff = 250 * time.Millisecond
	fanoutMaxBackoff = 30 * time.Second

	// fanoutDropWarnInterval rate limits the "queue full" warning. A full queue
	// means the database is already struggling, and a line per dropped notice
	// would make that worse.
	fanoutDropWarnInterval = time.Minute
)

// logNotice points at the rows a log batch wrote. It carries no content: the
// notification budget is 8000 bytes and a batch of agent output is not bounded
// by anything close to that, so the replica that has subscribers reads the
// rows back instead.
type logNotice struct {
	Origin    string `json:"o"`
	SessionID string `json:"s"`
	RunID     string `json:"r"`
	From      int64  `json:"lo"`
	To        int64  `json:"hi"`
}

// eventNotice carries a bus event whole.
//
// Unlike a log line an event is not a row anywhere, so there is nothing to
// re-read: it is carried or it is lost. When the payload will not fit, the
// event still goes out with its data omitted - the type and the resource id are
// what a subscriber routes on, and the resource itself is one API call away.
type eventNotice struct {
	Origin       string          `json:"o"`
	Type         string          `json:"ty"`
	ResourceID   string          `json:"rid"`
	ResourceType string          `json:"rty"`
	TenantID     *string         `json:"tid,omitempty"`
	Timestamp    time.Time       `json:"ts"`
	Data         json.RawMessage `json:"d,omitempty"`
	DataOmitted  bool            `json:"omit,omitempty"`
}

// LiveFanoutConfig configures the relay.
type LiveFanoutConfig struct {
	// Notifier is the LISTEN/NOTIFY transport.
	Notifier store.Notifier
	// Store reads back the rows a log notice points at.
	Store store.Store
	// Logs and Events are the local subscriber managers a remote notice is
	// delivered into.
	Logs   *LogSubscriberManager
	Events *EventBus
	// ReplicaID identifies this process, and is what makes self-suppression
	// exact rather than heuristic.
	ReplicaID string
	// Logger is required.
	Logger *zap.Logger
}

// LiveFanout re-broadcasts logs and events across replicas.
type LiveFanout struct {
	notifier  store.Notifier
	store     store.Store
	logs      *LogSubscriberManager
	events    *EventBus
	replicaID string
	logger    *zap.Logger

	queue chan store.Notification

	// delivered is signalled after every notice that was accepted from a peer.
	// Tests wait on it; production leaves it nil.
	delivered func(channel string)

	mu           sync.Mutex
	drops        int64
	lastDropWarn time.Time

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewLiveFanout builds the relay. It does not touch the database; Start does.
func NewLiveFanout(cfg LiveFanoutConfig) (*LiveFanout, error) {
	if cfg.Notifier == nil {
		return nil, errors.New("core: fan-out needs a notifier")
	}
	if cfg.Logger == nil {
		return nil, ErrLoggerRequired
	}

	return &LiveFanout{
		notifier:  cfg.Notifier,
		store:     cfg.Store,
		logs:      cfg.Logs,
		events:    cfg.Events,
		replicaID: cfg.ReplicaID,
		logger:    cfg.Logger,
		queue:     make(chan store.Notification, fanoutQueueDepth),
		stop:      make(chan struct{}),
	}, nil
}

// Start launches the publish and listen loops.
func (f *LiveFanout) Start(ctx context.Context) error {
	f.wg.Add(2)
	go func() {
		defer f.wg.Done()
		f.publishLoop(ctx)
	}()
	go func() {
		defer f.wg.Done()
		f.listenLoop(ctx)
	}()

	f.logger.Info("live fan-out started",
		zap.String("replica_id", f.replicaID),
		zap.Strings("channels", []string{FanoutLogChannel, FanoutEventChannel}),
	)
	return nil
}

// Stop drains both loops. Notices still queued are dropped: they are a live
// tail, and a tail nobody is watching any more is not worth holding shutdown
// open for.
func (f *LiveFanout) Stop(context.Context) {
	f.stopOnce.Do(func() { close(f.stop) })
	f.wg.Wait()
}

// PublishLogs announces a freshly written log batch to the other replicas.
//
// One notice per (session, run) group rather than per line: sequence is unique
// per run, so a group is what the read-back on the other side can express.
func (f *LiveFanout) PublishLogs(logs []*store.Log) {
	if len(logs) == 0 {
		return
	}

	type span struct {
		sessionID string
		from, to  int64
	}
	groups := make(map[string]*span)
	order := make([]string, 0, 1)

	for _, log := range logs {
		if log == nil || log.RunID == "" {
			continue
		}
		existing, ok := groups[log.RunID]
		if !ok {
			groups[log.RunID] = &span{sessionID: log.SessionID, from: log.Sequence, to: log.Sequence}
			order = append(order, log.RunID)
			continue
		}
		if log.Sequence < existing.from {
			existing.from = log.Sequence
		}
		if log.Sequence > existing.to {
			existing.to = log.Sequence
		}
	}

	for _, runID := range order {
		group := groups[runID]
		f.enqueue(FanoutLogChannel, logNotice{
			Origin:    f.replicaID,
			SessionID: group.sessionID,
			RunID:     runID,
			From:      group.from,
			To:        group.to,
		})
	}
}

// PublishEvent announces a bus event to the other replicas.
func (f *LiveFanout) PublishEvent(evt Event) {
	notice := eventNotice{
		Origin:       f.replicaID,
		Type:         evt.Type,
		ResourceID:   evt.ResourceID,
		ResourceType: evt.ResourceType,
		TenantID:     evt.TenantID,
		Timestamp:    evt.Timestamp,
	}
	if notice.Timestamp.IsZero() {
		notice.Timestamp = time.Now()
	}

	if evt.Data != nil {
		if encoded, err := json.Marshal(evt.Data); err == nil {
			notice.Data = encoded
		} else {
			notice.DataOmitted = true
			f.logger.Warn("event payload could not be encoded for the other replicas",
				zap.String("event_type", evt.Type), zap.Error(err))
		}
	}

	// Oversized payloads shed their data rather than being dropped: an SSE
	// consumer routes on the type and the resource id, and can fetch the rest.
	if encoded, err := json.Marshal(notice); err == nil && len(encoded) > store.MaxNotifyPayload {
		notice.Data = nil
		notice.DataOmitted = true
		f.logger.Warn("event payload is too large to carry across replicas; sending it without data",
			zap.String("event_type", evt.Type),
			zap.String("resource_id", evt.ResourceID),
			zap.Int("payload_bytes", len(encoded)),
		)
	}

	f.enqueue(FanoutEventChannel, notice)
}

// enqueue encodes a notice and hands it to the publish loop, never blocking.
func (f *LiveFanout) enqueue(channel string, notice any) {
	payload, err := json.Marshal(notice)
	if err != nil {
		f.logger.Warn("could not encode a fan-out notice",
			zap.String("channel", channel), zap.Error(err))
		return
	}

	select {
	case f.queue <- store.Notification{Channel: channel, Payload: string(payload)}:
	default:
		f.recordDrop(channel)
	}
}

func (f *LiveFanout) recordDrop(channel string) {
	f.mu.Lock()
	f.drops++
	total := f.drops
	warn := time.Since(f.lastDropWarn) > fanoutDropWarnInterval
	if warn {
		f.lastDropWarn = time.Now()
	}
	f.mu.Unlock()

	if warn {
		f.logger.Warn("live fan-out is behind; tails on other replicas will skip",
			zap.String("channel", channel),
			zap.Int64("dropped_total", total),
		)
	}
}

// Drops reports how many notices the publish queue could not take. Used by
// tests and by anyone reading the warning above.
func (f *LiveFanout) Drops() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drops
}

func (f *LiveFanout) publishLoop(ctx context.Context) {
	for {
		select {
		case <-f.stop:
			return
		case <-ctx.Done():
			return
		case notification := <-f.queue:
			publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fanoutReadTimeout)
			err := f.notifier.Notify(publishCtx, notification.Channel, notification.Payload)
			cancel()
			if err != nil {
				// Nothing to retry into: by the time a retry landed the tail
				// would have moved on, and the rows are in the database.
				f.logger.Debug("could not publish a fan-out notice",
					zap.String("channel", notification.Channel), zap.Error(err))
			}
		}
	}
}

// listenLoop keeps a listener connection open, reconnecting with backoff.
//
// Every reconnection is a gap: notifications sent while the connection was down
// were delivered to nobody and are gone. That is the deal LISTEN/NOTIFY makes,
// and it is why nothing here is load bearing - the rows never lie.
func (f *LiveFanout) listenLoop(ctx context.Context) {
	backoff := fanoutMinBackoff

	for {
		select {
		case <-f.stop:
			return
		case <-ctx.Done():
			return
		default:
		}

		stream, err := f.notifier.Listen(ctx, FanoutLogChannel, FanoutEventChannel)
		if err != nil {
			f.logger.Warn("could not open the fan-out listener; live tails on this replica are local-only until it reconnects",
				zap.Duration("retry_in", backoff), zap.Error(err))
			if !f.sleep(backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		backoff = fanoutMinBackoff
		f.consume(ctx, stream)

		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fanoutReadTimeout)
		_ = stream.Close(closeCtx)
		cancel()
	}
}

// consume reads one listener connection until it breaks or the relay stops.
func (f *LiveFanout) consume(ctx context.Context, stream store.NotificationStream) {
	// The Next call blocks in the driver, so stopping needs a context it
	// watches rather than a select around it.
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	watcher := make(chan struct{})
	go func() {
		defer close(watcher)
		select {
		case <-f.stop:
			cancel()
		case <-waitCtx.Done():
		}
	}()
	defer func() { cancel(); <-watcher }()

	for {
		notification, err := stream.Next(waitCtx)
		if err != nil {
			if waitCtx.Err() == nil {
				f.logger.Warn("the fan-out listener dropped; reconnecting", zap.Error(err))
			}
			return
		}
		f.dispatch(ctx, notification)
	}
}

func (f *LiveFanout) sleep(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-f.stop:
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > fanoutMaxBackoff {
		return fanoutMaxBackoff
	}
	return next
}

// dispatch delivers one notice into this replica's subscribers.
func (f *LiveFanout) dispatch(ctx context.Context, notification store.Notification) {
	switch notification.Channel {
	case FanoutLogChannel:
		var notice logNotice
		if err := json.Unmarshal([]byte(notification.Payload), &notice); err != nil {
			f.logger.Warn("could not decode a log notice", zap.Error(err))
			return
		}
		if f.isSelf(notice.Origin) {
			return
		}
		f.replayLogs(ctx, notice)

	case FanoutEventChannel:
		var notice eventNotice
		if err := json.Unmarshal([]byte(notification.Payload), &notice); err != nil {
			f.logger.Warn("could not decode an event notice", zap.Error(err))
			return
		}
		if f.isSelf(notice.Origin) {
			return
		}
		f.deliverEvent(notice)

	default:
		f.logger.Debug("ignoring a notification on an unknown channel",
			zap.String("channel", notification.Channel))
	}
}

// isSelf reports whether this process published the notice.
//
// Postgres delivers a notification to every listening session including the
// publisher's own process, so without this every local subscriber would receive
// each line twice - once from the local broadcast and once from the relay.
func (f *LiveFanout) isSelf(origin string) bool {
	return origin != "" && origin == f.replicaID
}

// replayLogs reads back the rows a notice points at and broadcasts them.
func (f *LiveFanout) replayLogs(ctx context.Context, notice logNotice) {
	if f.logs == nil || f.store == nil || notice.RunID == "" {
		return
	}

	// The cheap check first: a replica nobody is following this session on does
	// no database work at all, which is what makes the relay affordable to
	// leave always on.
	if f.logs.SubscriberCount(notice.SessionID) == 0 {
		return
	}

	// System access, for the same reason the replica registry uses it: the
	// relay serves every tenant's sessions and has no request to take a tenant
	// from. Authorisation happened when the subscriber subscribed.
	readCtx, cancel := context.WithTimeout(
		store.WithSystemAccess(context.WithoutCancel(ctx)), fanoutReadTimeout)
	defer cancel()

	from, to := notice.From, notice.To
	result, err := f.store.ListLogs(readCtx, store.ListLogsOptions{
		BaseListOptions: store.BaseListOptions{Limit: fanoutLogReadLimit},
		RunID:           &notice.RunID,
		MinSequence:     &from,
		MaxSequence:     &to,
	})
	if err != nil {
		f.logger.Warn("could not read back a notified log batch; this replica's tail skips it",
			zap.String("session_id", notice.SessionID),
			zap.String("run_id", notice.RunID),
			zap.Error(err),
		)
		return
	}
	if result.HasMore {
		f.logger.Debug("a notified log batch was larger than one read; the tail skips the remainder",
			zap.String("run_id", notice.RunID),
			zap.Int64("from", from),
			zap.Int64("to", to),
		)
	}

	// Ordering is asserted here rather than assumed from the store's default
	// sort: a tail delivered out of order is worse than no tail.
	items := result.Items
	sort.SliceStable(items, func(i, j int) bool { return items[i].Sequence < items[j].Sequence })

	for _, log := range items {
		f.logs.Broadcast(log)
	}
	f.signal(FanoutLogChannel)
}

// deliverEvent hands a peer's event to the local bus without re-publishing it.
func (f *LiveFanout) deliverEvent(notice eventNotice) {
	if f.events == nil {
		return
	}

	evt := Event{
		Type:         notice.Type,
		ResourceID:   notice.ResourceID,
		ResourceType: notice.ResourceType,
		TenantID:     notice.TenantID,
		Timestamp:    notice.Timestamp,
	}
	if len(notice.Data) > 0 {
		// Kept as raw JSON: the websocket adapter renders payloads through JSON
		// anyway, so re-hydrating the original Go type would buy nothing and
		// require this package to know every event's shape.
		evt.Data = notice.Data
	}

	f.events.deliver(evt)
	f.signal(FanoutEventChannel)
}

func (f *LiveFanout) signal(channel string) {
	if f.delivered != nil {
		f.delivered(channel)
	}
}
