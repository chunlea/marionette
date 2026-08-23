package core

import (
	"context"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/webhook"
	"go.uber.org/zap"
)

// eventBufferSize is how far a subscriber may fall behind before its events
// start being dropped. The event stream is a best-effort observability feed,
// never a delivery guarantee - webhooks are the durable path.
const eventBufferSize = 64

// Event is a resource lifecycle event published in-process.
// It carries the same payload the webhook dispatcher sees, so the WebSocket
// event stream and webhook subscribers observe the same facts.
type Event struct {
	Type         string
	ResourceID   string
	ResourceType string
	Data         any
	TenantID     *string
	Timestamp    time.Time
}

// EventBus fans resource events out to in-process subscribers.
// The zero value is not usable; call NewEventBus.
type EventBus struct {
	mu      sync.RWMutex
	nextID  uint64
	subs    map[uint64]*eventSubscriber
	matcher *webhook.Matcher
	logger  *zap.Logger
}

type eventSubscriber struct {
	ch chan Event
	// types are event-type patterns using webhook matcher semantics
	// ("task.created", "task.*", "task"). Empty means every event.
	types []string
}

// NewEventBus creates an EventBus.
func NewEventBus(logger *zap.Logger) *EventBus {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &EventBus{
		subs:    make(map[uint64]*eventSubscriber),
		matcher: webhook.NewMatcher(),
		logger:  logger,
	}
}

// Subscribe returns a channel of events matching the given type patterns and a
// function that unsubscribes and closes the channel. The unsubscribe function
// is safe to call more than once.
func (b *EventBus) Subscribe(types []string) (<-chan Event, func()) {
	sub := &eventSubscriber{
		ch:    make(chan Event, eventBufferSize),
		types: types,
	}

	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.subs[id] = sub
	b.mu.Unlock()

	var once sync.Once
	return sub.ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
			close(sub.ch)
		})
	}
}

// Publish fans an event out to every matching subscriber. It never blocks:
// a subscriber that cannot keep up loses the event.
func (b *EventBus) Publish(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subs {
		if len(sub.types) > 0 && !b.matcher.Matches(evt.Type, sub.types) {
			continue
		}
		select {
		case sub.ch <- evt:
		default:
			b.logger.Warn("dropping event for slow subscriber",
				zap.String("event_type", evt.Type),
				zap.String("resource_id", evt.ResourceID),
			)
		}
	}
}

// SubscriberCount reports the number of active subscribers. Used by tests and
// monitoring.
func (b *EventBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// EventDispatcher tees every webhook dispatch into the in-process event bus.
// Managers hold a WebhookIntegration built on top of this, so publishing to
// subscribers costs nothing at the call sites.
type EventDispatcher struct {
	webhooks WebhookDispatcher
	bus      *EventBus
}

// NewEventDispatcher creates a dispatcher that forwards to webhooks and the bus.
// Either side may be nil.
func NewEventDispatcher(webhooks WebhookDispatcher, bus *EventBus) *EventDispatcher {
	return &EventDispatcher{webhooks: webhooks, bus: bus}
}

// Dispatch publishes to the bus first (cheap, non-blocking) and then hands the
// event to the durable webhook path.
func (d *EventDispatcher) Dispatch(
	ctx context.Context,
	eventType string,
	resource webhook.ResourceInfo,
	data any,
	tenantID *string,
) error {
	if d.bus != nil {
		d.bus.Publish(Event{
			Type:         eventType,
			ResourceID:   resource.ID,
			ResourceType: resource.Type,
			Data:         data,
			TenantID:     tenantID,
		})
	}

	if d.webhooks == nil {
		return nil
	}
	return d.webhooks.Dispatch(ctx, eventType, resource, data, tenantID)
}
