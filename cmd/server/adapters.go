package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/cryptoutil"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/server/admin"
	"github.com/chunlea/marionette/pkg/server/api"
	"github.com/chunlea/marionette/pkg/server/core"
	grpcserver "github.com/chunlea/marionette/pkg/server/grpc"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/store/postgres"
	"github.com/chunlea/marionette/pkg/tunnel"
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

// The two adapters below back the /admin/api/v1/agent-configs and
// /admin/api/v1/provider-configs routes, which answered 501 for the whole life
// of the project because admin.WithAgentConfigService and
// WithProviderConfigService were never called. Runner spawn takes a
// provider_config_id, and nothing could create one, so admin runner spawn was
// unusable end to end even after the spawn route itself was wired.
//
// They live here rather than beside admin.ProfileAdapter only to keep this
// round's lane boundaries clean; they depend on nothing admin cannot see.

// providerConfigStore is the slice of the store provider configs need.
type providerConfigStore interface {
	CreateProviderConfig(ctx context.Context, config *store.ProviderConfig) error
	GetProviderConfig(ctx context.Context, id string) (*store.ProviderConfig, error)
	ListProviderConfigs(ctx context.Context, opts store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error)
	UpdateProviderConfig(ctx context.Context, id string, updates store.ProviderConfigUpdates) error
	DeleteProviderConfig(ctx context.Context, id string) error
}

// providerConfigAdapter implements admin.ProviderConfigService over the store.
type providerConfigAdapter struct {
	store providerConfigStore
}

func newProviderConfigAdapter(s providerConfigStore) *providerConfigAdapter {
	return &providerConfigAdapter{store: s}
}

// Create stores a new provider configuration.
func (a *providerConfigAdapter) Create(ctx context.Context, opts admin.CreateProviderConfigOptions) (*store.ProviderConfig, error) {
	now := time.Now()
	cfg := &store.ProviderConfig{
		ID:            id.ProviderConfig(),
		Name:          opts.Name,
		Provider:      opts.Provider,
		Config:        marshalObject(opts.Config),
		SuspendConfig: marshalObject(opts.SuspendConfig),
		IsDefault:     opts.IsDefault,
		Labels:        marshalLabels(opts.Labels),
		Annotations:   json.RawMessage("{}"),
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := a.store.CreateProviderConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Get retrieves a provider configuration by ID.
func (a *providerConfigAdapter) Get(ctx context.Context, configID string) (*store.ProviderConfig, error) {
	return a.store.GetProviderConfig(ctx, configID)
}

// List returns provider configurations matching opts.
func (a *providerConfigAdapter) List(ctx context.Context, opts admin.ListProviderConfigsOptions) (*admin.ListResult[store.ProviderConfig], error) {
	var provider *string
	if opts.Provider != "" {
		provider = &opts.Provider
	}

	result, err := a.store.ListProviderConfigs(ctx, store.ListProviderConfigsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Provider: provider,
	})
	if err != nil {
		return nil, err
	}

	return &admin.ListResult[store.ProviderConfig]{
		Items:      result.Items,
		NextCursor: result.NextCursor,
		TotalCount: result.TotalCount,
	}, nil
}

// Update applies opts to an existing provider configuration.
func (a *providerConfigAdapter) Update(ctx context.Context, configID string, opts admin.UpdateProviderConfigOptions) (*store.ProviderConfig, error) {
	updates := store.ProviderConfigUpdates{
		Name:      opts.Name,
		IsDefault: opts.IsDefault,
	}
	if opts.Config != nil {
		updates.Config = marshalObject(*opts.Config)
	}
	if opts.SuspendConfig != nil {
		updates.SuspendConfig = marshalObject(*opts.SuspendConfig)
	}
	if opts.Labels != nil {
		updates.Labels = marshalLabels(*opts.Labels)
	}

	if err := a.store.UpdateProviderConfig(ctx, configID, updates); err != nil {
		return nil, err
	}
	return a.store.GetProviderConfig(ctx, configID)
}

// Delete removes a provider configuration.
func (a *providerConfigAdapter) Delete(ctx context.Context, configID string) error {
	return a.store.DeleteProviderConfig(ctx, configID)
}

// agentConfigStore is the slice of the store agent configs need.
type agentConfigStore interface {
	CreateAgentConfig(ctx context.Context, config *store.AgentConfig) error
	GetAgentConfig(ctx context.Context, id string) (*store.AgentConfig, error)
	ListAgentConfigs(ctx context.Context, opts store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error)
	UpdateAgentConfig(ctx context.Context, id string, updates store.AgentConfigUpdates) error
	DeleteAgentConfig(ctx context.Context, id string) error
}

// secretEncryptor encrypts an agent credential at rest. It is the reason this
// service is wired only when an encryption key is configured: an agent config
// is an API key with a name attached, and storing one in plaintext because a
// key was missing would be worse than the 501 it replaces.
type secretEncryptor interface {
	EncryptString(ctx context.Context, resourceType, resourceID, plaintext string) (string, error)
}

// agentConfigAdapter implements admin.AgentConfigService over the store,
// encrypting the API key on the way in.
type agentConfigAdapter struct {
	store  agentConfigStore
	crypto secretEncryptor
}

func newAgentConfigAdapter(s agentConfigStore, crypto secretEncryptor) *agentConfigAdapter {
	return &agentConfigAdapter{store: s, crypto: crypto}
}

// agentConfigResource is the resource type the credential's data key is
// derived for. Each config gets its own DEK, so revoking one config's key does
// not touch another's.
const agentConfigResource = "agent_config"

// Create stores a new agent configuration with its API key encrypted.
func (a *agentConfigAdapter) Create(ctx context.Context, opts admin.CreateAgentConfigOptions) (*store.AgentConfig, error) {
	now := time.Now()
	cfg := &store.AgentConfig{
		ID:          id.AgentConfig(),
		Name:        opts.Name,
		Agent:       opts.Agent,
		Model:       optionalString(opts.Model),
		BaseURL:     optionalString(opts.BaseURL),
		Extra:       marshalObject(opts.Extra),
		IsDefault:   opts.IsDefault,
		Labels:      marshalLabels(opts.Labels),
		Annotations: json.RawMessage("{}"),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if opts.APIKey != "" {
		encrypted, err := a.crypto.EncryptString(ctx, agentConfigResource, cfg.ID, opts.APIKey)
		if err != nil {
			return nil, fmt.Errorf("encrypting the agent API key: %w", err)
		}
		cfg.APIKeyEncrypted = encrypted
	}

	if err := a.store.CreateAgentConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Get retrieves an agent configuration by ID.
func (a *agentConfigAdapter) Get(ctx context.Context, configID string) (*store.AgentConfig, error) {
	return a.store.GetAgentConfig(ctx, configID)
}

// List returns agent configurations matching opts.
func (a *agentConfigAdapter) List(ctx context.Context, opts admin.ListAgentConfigsOptions) (*admin.ListResult[store.AgentConfig], error) {
	var agent *string
	if opts.Agent != "" {
		agent = &opts.Agent
	}

	result, err := a.store.ListAgentConfigs(ctx, store.ListAgentConfigsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Agent: agent,
	})
	if err != nil {
		return nil, err
	}

	return &admin.ListResult[store.AgentConfig]{
		Items:      result.Items,
		NextCursor: result.NextCursor,
		TotalCount: result.TotalCount,
	}, nil
}

// Update applies opts to an existing agent configuration, re-encrypting the
// API key when one is supplied.
func (a *agentConfigAdapter) Update(ctx context.Context, configID string, opts admin.UpdateAgentConfigOptions) (*store.AgentConfig, error) {
	updates := store.AgentConfigUpdates{
		Name:      opts.Name,
		Model:     opts.Model,
		BaseURL:   opts.BaseURL,
		IsDefault: opts.IsDefault,
	}
	if opts.Extra != nil {
		updates.Extra = marshalObject(*opts.Extra)
	}
	if opts.Labels != nil {
		updates.Labels = marshalLabels(*opts.Labels)
	}
	if opts.APIKey != nil {
		encrypted, err := a.crypto.EncryptString(ctx, agentConfigResource, configID, *opts.APIKey)
		if err != nil {
			return nil, fmt.Errorf("encrypting the agent API key: %w", err)
		}
		updates.APIKeyEncrypted = &encrypted
	}

	if err := a.store.UpdateAgentConfig(ctx, configID, updates); err != nil {
		return nil, err
	}
	return a.store.GetAgentConfig(ctx, configID)
}

// Delete removes an agent configuration.
func (a *agentConfigAdapter) Delete(ctx context.Context, configID string) error {
	return a.store.DeleteAgentConfig(ctx, configID)
}

// marshalObject renders a free-form object column. A nil map is "{}" rather
// than SQL NULL: the columns are NOT NULL and every reader json.Unmarshals
// them without a nil check.
func marshalObject(v map[string]any) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("{}")
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}

// marshalLabels renders a label map the same way.
func marshalLabels(v map[string]string) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("{}")
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}

// optionalString maps an empty string to a nil pointer, which is how the
// nullable columns spell "unset".
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Compile-time checks: an interface change in pkg/server/admin must fail the
// build here, not at the 501 a route starts answering again.
var (
	_ admin.ProviderConfigService = (*providerConfigAdapter)(nil)
	_ admin.AgentConfigService    = (*agentConfigAdapter)(nil)
)

// adminDeps is everything the admin services are built from. Every field is
// optional: a server started without a database still serves health.
type adminDeps struct {
	store       store.Store
	app         *core.App
	apiKeys     *auth.APIKeyService
	crypto      secretEncryptor
	health      admin.HealthService
	streamMgr   *core.StreamManager
	connManager *grpcserver.ConnectionManager
	logger      *zap.Logger
}

// adminServices holds one value per admin.WithX service option.
//
// It exists so that "which admin services does the production binary attach"
// is a value a test can inspect, instead of a sequence of appends buried in
// main. The regression it guards is concrete: WithProviderConfigService was
// never called, so every /admin/api/v1/provider-configs route answered 501 -
// and admin runner spawn takes a provider_config_id, so the spawn route that
// WAS wired could not be driven end to end.
type adminServices struct {
	health           admin.HealthService
	apiKeys          admin.APIKeyService
	agentConfigs     admin.AgentConfigService
	providerConfigs  admin.ProviderConfigService
	profiles         admin.ProfileService
	runners          admin.RunnerAdminService
	runnerTokens     admin.RunnerTokenAdminService
	sessionActivator admin.SessionActivator
	actionLogs       admin.ActionLogService
	webhooks         admin.WebhookService
	streams          *admin.StreamsHandler
	signaling        *admin.SignalingHandler
}

// buildAdminServices assembles the admin services this deployment can offer.
//
// A service is left nil only when the thing backing it is genuinely absent
// (no database, no core app, streaming switched off) - never because nobody
// remembered to call its option.
func buildAdminServices(d adminDeps) adminServices {
	svc := adminServices{health: d.health}

	if d.apiKeys != nil {
		svc.apiKeys = admin.NewAPIKeyAdapter(d.apiKeys)
	}

	if d.store != nil {
		svc.actionLogs = admin.NewActionLogStoreAdapter(d.store)
		svc.runnerTokens = admin.NewRunnerTokenAdapter(auth.NewRunnerTokenService(d.store, id.RunnerToken))
		svc.profiles = admin.NewProfileAdapter(d.store)
		svc.providerConfigs = newProviderConfigAdapter(d.store)

		// Agent configs are only offered when credentials can be encrypted.
		// The alternative - writing the operator's API key to the database in
		// plaintext because MARIONETTE_ENCRYPTION_KEY was unset - is worse
		// than the 501 it would replace.
		if d.crypto != nil {
			svc.agentConfigs = newAgentConfigAdapter(d.store, d.crypto)
		} else if d.logger != nil {
			d.logger.Warn("agent config admin routes disabled: MARIONETTE_ENCRYPTION_KEY is not set, " +
				"and an agent config is an API key that must not be stored in plaintext")
		}
	}

	if d.app != nil {
		svc.sessionActivator = d.app.Sessions
		// Without this the admin runner endpoints answered 501 and no managed
		// runner could ever be spawned through the API, which is why nothing
		// recorded runners.provider_instance_id in the first place.
		svc.runners = &runnerAdminAdapter{provisioner: d.app.RunnerProvisioner}
		// The webhook manager and its integration are built by core.Wire so the
		// managers get them at construction time instead of through setters.
		svc.webhooks = admin.NewWebhookAdapter(d.app.Webhooks)
	}

	if d.streamMgr != nil {
		// connManager is what lets the handler send StartDesktopStream to the
		// agent rather than only recording the stream in the database.
		svc.streams = admin.NewStreamsHandler(d.streamMgr, d.connManager, d.logger)
		if sfu := d.streamMgr.GetSignalingHandler(); sfu != nil {
			svc.signaling = admin.NewSignalingHandler(sfu, admin.DefaultSignalingConfig(), d.logger)
		}
	}

	return svc
}

// options renders the services as admin options.
//
// Nil fields are skipped rather than passed through: handing WithXService a
// nil implementation stores a non-nil interface, so the handler's "is this
// service configured" guard would pass on its way to a nil dereference.
func (s adminServices) options() []admin.Option {
	var opts []admin.Option
	if s.health != nil {
		opts = append(opts, admin.WithHealthService(s.health))
	}
	if s.apiKeys != nil {
		opts = append(opts, admin.WithAPIKeyService(s.apiKeys))
	}
	if s.agentConfigs != nil {
		opts = append(opts, admin.WithAgentConfigService(s.agentConfigs))
	}
	if s.providerConfigs != nil {
		opts = append(opts, admin.WithProviderConfigService(s.providerConfigs))
	}
	if s.profiles != nil {
		opts = append(opts, admin.WithProfileService(s.profiles))
	}
	if s.runners != nil {
		opts = append(opts, admin.WithRunnerAdminService(s.runners))
	}
	if s.runnerTokens != nil {
		opts = append(opts, admin.WithRunnerTokenAdminService(s.runnerTokens))
	}
	if s.sessionActivator != nil {
		opts = append(opts, admin.WithSessionActivator(s.sessionActivator))
	}
	if s.actionLogs != nil {
		opts = append(opts, admin.WithActionLogService(s.actionLogs))
	}
	if s.webhooks != nil {
		opts = append(opts, admin.WithWebhookService(s.webhooks))
	}
	if s.streams != nil {
		opts = append(opts, admin.WithStreamsHandler(s.streams))
	}
	if s.signaling != nil {
		opts = append(opts, admin.WithSignalingHandler(s.signaling))
	}
	return opts
}

// storeOrNil keeps a nil *postgres.Store from becoming a non-nil store.Store.
// Without it every "is there a database" check downstream reads true.
func storeOrNil(s *postgres.Store) store.Store {
	if s == nil {
		return nil
	}
	return s
}

// tunnelStoreAdapter gives the tunnel manager a database.
//
// Until now cmd/server built the manager without one, so store was nil: Create
// never wrote the tunnels table, every read-through path in pkg/tunnel
// dead-ended, and tunnels were memory-only - a server restart lost every
// tunnel that was still open, and the caller got "tunnel not found" for a URL
// it had just been handed.
//
// Two fields the tunnel package's Store interface does not carry have to be
// supplied here:
//
//   - token_prefix is NOT NULL. It is the human-readable head of the token,
//     and it is derivable from the token itself, which the tunnel being
//     created still carries. Reconstructed with the same rule cryptoutil uses
//     so what lands in the column matches what the manager logs.
//   - tenant_id stays NULL. Tunnels are not tenant-scoped yet (D2 deferred),
//     and writing a tenant here would be inventing an isolation boundary the
//     rest of the tunnel path does not enforce.
type tunnelStoreAdapter struct {
	store tunnelStore
}

// tunnelStore is the slice of the database tunnels need. It is narrower than
// store.Store because DeleteExpiredTunnels lives only on the Postgres store.
type tunnelStore interface {
	CreateTunnel(ctx context.Context, t *store.Tunnel) error
	GetTunnel(ctx context.Context, id string) (*store.Tunnel, error)
	GetTunnelByTokenHash(ctx context.Context, hash string) (*store.Tunnel, error)
	ListTunnels(ctx context.Context, opts store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error)
	UpdateTunnel(ctx context.Context, id string, updates store.TunnelUpdates) error
	DeleteExpiredTunnels(ctx context.Context) (int64, error)
}

func newTunnelStoreAdapter(s tunnelStore) *tunnelStoreAdapter {
	return &tunnelStoreAdapter{store: s}
}

// tunnelListLimit bounds one read-through page.
//
// The manager asks for a session's tunnels, and a session with more than this
// many live tunnels is pathological rather than merely busy. Bounded reads
// beat an unbounded scan that only shows up under load.
const tunnelListLimit = 200

// CreateTunnel persists a newly created tunnel.
func (a *tunnelStoreAdapter) CreateTunnel(ctx context.Context, t *tunnel.Tunnel, tokenHash string, hashVersion int) error {
	row := &store.Tunnel{
		ID:          t.ID,
		SessionID:   t.SessionID,
		RunnerID:    optionalString(t.RunnerID),
		Type:        t.Type,
		Direction:   t.Direction,
		LocalPort:   t.LocalPort,
		PublicURL:   optionalString(t.PublicURL),
		IsPublic:    t.IsPublic,
		TokenHash:   tokenHash,
		TokenPrefix: tunnelTokenPrefix(t.Token),
		HashVersion: hashVersion,
		CreatedAt:   t.CreatedAt,
		ExpiresAt:   t.ExpiresAt,
		ClosedAt:    t.ClosedAt,
	}
	return a.store.CreateTunnel(ctx, row)
}

// GetTunnel retrieves a tunnel by ID.
func (a *tunnelStoreAdapter) GetTunnel(ctx context.Context, id string) (*tunnel.Tunnel, error) {
	row, err := a.store.GetTunnel(ctx, id)
	if err != nil {
		return nil, err
	}
	return toTunnel(row), nil
}

// GetTunnelByHash retrieves a tunnel by its token hash. This is what lets a
// token authenticate itself after a restart, when the in-memory entry that
// held the hash is gone.
func (a *tunnelStoreAdapter) GetTunnelByHash(ctx context.Context, hash string) (*tunnel.Tunnel, error) {
	row, err := a.store.GetTunnelByTokenHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	return toTunnel(row), nil
}

// ListTunnels returns tunnels matching opts.
func (a *tunnelStoreAdapter) ListTunnels(ctx context.Context, opts tunnel.ListOptions) ([]*tunnel.Tunnel, error) {
	storeOpts := store.ListTunnelsOptions{
		BaseListOptions: store.BaseListOptions{Limit: tunnelListLimit},
		Type:            opts.Types,
		IncludeClosed:   opts.IncludeClosed,
	}
	if opts.SessionID != "" {
		storeOpts.SessionID = &opts.SessionID
	}
	if opts.RunnerID != "" {
		storeOpts.RunnerID = &opts.RunnerID
	}

	result, err := a.store.ListTunnels(ctx, storeOpts)
	if err != nil {
		return nil, err
	}

	tunnels := make([]*tunnel.Tunnel, 0, len(result.Items))
	for _, row := range result.Items {
		tunnels = append(tunnels, toTunnel(row))
	}
	return tunnels, nil
}

// UpdateTunnel applies updates to a stored tunnel.
func (a *tunnelStoreAdapter) UpdateTunnel(ctx context.Context, id string, updates tunnel.Updates) error {
	return a.store.UpdateTunnel(ctx, id, store.TunnelUpdates{
		PublicURL: updates.PublicURL,
		ClosedAt:  updates.ClosedAt,
	})
}

// DeleteExpiredTunnels removes tunnels past their expiry.
func (a *tunnelStoreAdapter) DeleteExpiredTunnels(ctx context.Context) (int64, error) {
	return a.store.DeleteExpiredTunnels(ctx)
}

// toTunnel converts a stored row into the tunnel package's shape.
//
// Token is deliberately not set: it exists only in the response to the create
// that minted it, and a tunnel read back from the database has no plaintext
// token to offer.
func toTunnel(row *store.Tunnel) *tunnel.Tunnel {
	return &tunnel.Tunnel{
		ID:        row.ID,
		SessionID: row.SessionID,
		RunnerID:  stringValue(row.RunnerID),
		Type:      row.Type,
		Direction: row.Direction,
		LocalPort: row.LocalPort,
		PublicURL: stringValue(row.PublicURL),
		IsPublic:  row.IsPublic,
		ExpiresAt: row.ExpiresAt,
		CreatedAt: row.CreatedAt,
		ClosedAt:  row.ClosedAt,
	}
}

// tunnelTokenPrefix rebuilds the display prefix cryptoutil produced when the
// token was minted: the type prefix plus the first eight characters of the
// random part.
//
// token_prefix is NOT NULL, so a tunnel with no token still has to write
// something; the type prefix alone is the honest answer in that case.
func tunnelTokenPrefix(token string) string {
	const displayChars = 8

	typePrefix := cryptoutil.ExtractPrefix(token)
	if typePrefix == "" {
		typePrefix = cryptoutil.PrefixTunnelToken
	}
	if len(token) >= len(typePrefix)+displayChars {
		return token[:len(typePrefix)+displayChars]
	}
	if token != "" {
		return token
	}
	return typePrefix
}

// stringValue is the nil-safe read of an optional column.
func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Compile-time check: the tunnel package's Store interface must be satisfied
// here, not discovered to be unsatisfied at the nil-store dead end it replaces.
var _ tunnel.Store = (*tunnelStoreAdapter)(nil)
