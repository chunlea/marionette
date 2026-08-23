package main

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/chunlea/marionette/pkg/server/admin"
	"github.com/chunlea/marionette/pkg/server/api"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// The three adapters below back public API routes that returned 501 for the
// whole life of the project because api.WithRunnerService, WithLogStreamService
// and WithEventStreamService were never called. They live here rather than in
// pkg/server/api because api imports core, so core cannot import api back.

// runnerServiceAdapter implements api.RunnerService over the store.
type runnerServiceAdapter struct {
	store store.Store
}

func newRunnerServiceAdapter(s store.Store) *runnerServiceAdapter {
	return &runnerServiceAdapter{store: s}
}

// Get retrieves a runner by ID.
func (a *runnerServiceAdapter) Get(ctx context.Context, id string) (*store.Runner, error) {
	return a.store.GetRunner(ctx, id)
}

// List returns runners matching the filter options.
func (a *runnerServiceAdapter) List(ctx context.Context, opts api.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	storeOpts := store.ListRunnersOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Status: opts.Status,
		Labels: opts.Labels,
	}
	if opts.PoolName != "" {
		storeOpts.PoolName = &opts.PoolName
	}
	return a.store.ListRunners(ctx, storeOpts)
}

// logStreamAdapter implements api.LogStreamService on top of the core log
// subscriber manager.
//
// The two sides key differently: the subscriber manager fans out per session
// (that is what the gRPC log stream knows), while the API subscribes per task.
// The adapter resolves the task's session once, then filters the session feed
// down to that task.
type logStreamAdapter struct {
	store       store.Store
	subscribers *core.LogSubscriberManager
	logger      *zap.Logger
}

func newLogStreamAdapter(s store.Store, subs *core.LogSubscriberManager, logger *zap.Logger) *logStreamAdapter {
	return &logStreamAdapter{store: s, subscribers: subs, logger: logger}
}

// logStreamBuffer is how many log lines a websocket client may fall behind
// before lines start being dropped for it.
const logStreamBuffer = 256

// Subscribe subscribes to log messages for a task.
func (a *logStreamAdapter) Subscribe(ctx context.Context, taskID string) (<-chan api.LogMessage, func(), error) {
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}

	src := make(chan *store.Log, logStreamBuffer)
	out := make(chan api.LogMessage, logStreamBuffer)
	a.subscribers.Subscribe(task.SessionID, src)

	stop := make(chan struct{})
	var stopOnce sync.Once

	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case log, ok := <-src:
				if !ok {
					return
				}
				if log.TaskID != taskID {
					continue
				}
				msg := api.LogMessage{
					Type:      api.MessageTypeLog,
					TaskID:    log.TaskID,
					RunID:     log.RunID,
					Stream:    log.Stream,
					Level:     log.Level,
					Content:   log.Content,
					Sequence:  log.Sequence,
					Timestamp: log.CreatedAt,
				}
				select {
				case out <- msg:
				default:
					a.logger.Warn("dropping log for slow websocket client",
						zap.String("task_id", taskID),
						zap.Int64("sequence", log.Sequence),
					)
				}
			}
		}
	}()

	unsubscribe := func() {
		stopOnce.Do(func() {
			a.subscribers.Unsubscribe(task.SessionID, src)
			close(stop)
		})
	}
	return out, unsubscribe, nil
}

// eventStreamAdapter implements api.EventStreamService over the core event bus.
type eventStreamAdapter struct {
	bus    *core.EventBus
	logger *zap.Logger
}

func newEventStreamAdapter(bus *core.EventBus, logger *zap.Logger) *eventStreamAdapter {
	return &eventStreamAdapter{bus: bus, logger: logger}
}

// Subscribe subscribes to events matching the given filters.
func (a *eventStreamAdapter) Subscribe(ctx context.Context, opts api.EventSubscribeOptions) (<-chan api.EventMessage, func(), error) {
	src, unsubscribe := a.bus.Subscribe(opts.EventTypes)
	out := make(chan api.EventMessage, eventStreamBuffer)

	stop := make(chan struct{})
	var stopOnce sync.Once

	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case evt, ok := <-src:
				if !ok {
					return
				}
				select {
				case out <- toEventMessage(evt):
				default:
					a.logger.Warn("dropping event for slow websocket client",
						zap.String("event_type", evt.Type),
					)
				}
			}
		}
	}()

	return out, func() {
		stopOnce.Do(func() {
			unsubscribe()
			close(stop)
		})
	}, nil
}

// eventStreamBuffer is how many events a websocket client may fall behind
// before events start being dropped for it.
const eventStreamBuffer = 64

// toEventMessage converts a core event into its websocket representation.
// Event payloads are typed structs; they are rendered through JSON so the
// websocket contract does not depend on the Go type.
func toEventMessage(evt core.Event) api.EventMessage {
	msg := api.EventMessage{
		Type:      api.MessageTypeEvent,
		EventType: evt.Type,
		Resource:  evt.ResourceType,
		ID:        evt.ResourceID,
		Timestamp: evt.Timestamp,
	}
	if evt.Data == nil {
		return msg
	}
	if raw, err := json.Marshal(evt.Data); err == nil {
		data := map[string]any{}
		if err := json.Unmarshal(raw, &data); err == nil {
			msg.Data = data
		}
	}
	return msg
}

// runnerAdminAdapter implements admin.RunnerAdminService over the core runner
// provisioner.
//
// It lives here for the same reason as the adapters above: pkg/server/admin
// imports core, so core cannot import admin back to satisfy the interface
// itself.
type runnerAdminAdapter struct {
	provisioner *core.RunnerProvisioner
}

// Spawn creates a runner on a managed provider.
func (a *runnerAdminAdapter) Spawn(ctx context.Context, opts admin.SpawnRunnerOptions) (*store.Runner, error) {
	return a.provisioner.Spawn(ctx, core.ProvisionOptions{
		Name:             opts.Name,
		ProviderConfigID: opts.ProviderConfigID,
		ProviderName:     opts.Provider,
		ProfileID:        opts.ProfileID,
		Labels:           opts.Labels,
		WorkspaceMount:   opts.WorkspaceMount,
	})
}

// Destroy terminates a runner's instance.
func (a *runnerAdminAdapter) Destroy(ctx context.Context, id string) error {
	return a.provisioner.Destroy(ctx, id)
}

// Get retrieves a runner by ID.
func (a *runnerAdminAdapter) Get(ctx context.Context, id string) (*store.Runner, error) {
	return a.provisioner.Get(ctx, id)
}

// List returns runners matching opts.
func (a *runnerAdminAdapter) List(ctx context.Context, opts admin.ListRunnersOptions) (*admin.ListResult[store.Runner], error) {
	result, err := a.provisioner.List(ctx, store.ListRunnersOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Status: opts.Status,
		Labels: opts.Labels,
	})
	if err != nil {
		return nil, err
	}
	return &admin.ListResult[store.Runner]{
		Items:      result.Items,
		NextCursor: result.NextCursor,
		TotalCount: result.TotalCount,
	}, nil
}
