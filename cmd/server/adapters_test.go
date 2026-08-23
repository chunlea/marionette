package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/observability/health"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/server/admin"
	"github.com/chunlea/marionette/pkg/server/core"
	grpcserver "github.com/chunlea/marionette/pkg/server/grpc"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/store/storemock"
)

// fullAdminDeps builds the dependency set a fully configured production binary
// has: a database, a wired core app, an API key service, and credential
// crypto.
//
// The store is a strict mock with no expectations, which is itself an
// assertion: assembling the admin services must not touch the database.
func fullAdminDeps(t *testing.T) adminDeps {
	t.Helper()

	s := storemock.NewMockStore(gomock.NewController(t))
	logger := zap.NewNop()

	app, err := core.Wire(core.WireDeps{
		Store:              s,
		ConnManager:        grpcserver.NewConnectionManager(logger),
		CmdSender:          grpcserver.NewConnectionManager(logger),
		RunnerTokenService: auth.NewRunnerTokenService(s, id.RunnerToken),
		ProviderRegistry:   provider.NewRegistry(s),
		Logger:             logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	return adminDeps{
		store:       s,
		app:         app,
		apiKeys:     auth.NewAPIKeyService(s, id.APIKey),
		crypto:      stubEncryptor{},
		health:      health.NewChecker(),
		connManager: grpcserver.NewConnectionManager(logger),
		logger:      logger,
	}
}

// TestBuildAdminServices_AttachesEveryService is the anti-drift test for admin
// wiring, and the direct regression for the bug that motivated it: nobody
// called admin.WithProviderConfigService, so POST /admin/api/v1/provider-configs
// answered 501 - and admin runner spawn takes a provider_config_id, so the
// spawn route that WAS wired could not be driven end to end.
//
// Every field asserted here has a complete handler behind it. A nil field
// means a route answering 501 in a fully configured binary.
func TestBuildAdminServices_AttachesEveryService(t *testing.T) {
	svc := buildAdminServices(fullAdminDeps(t))

	assert.NotNil(t, svc.health, "health service")
	assert.NotNil(t, svc.apiKeys, "api key service")
	assert.NotNil(t, svc.agentConfigs, "agent config service")
	assert.NotNil(t, svc.providerConfigs, "provider config service")
	assert.NotNil(t, svc.profiles, "profile service")
	assert.NotNil(t, svc.runners, "runner admin service")
	assert.NotNil(t, svc.runnerTokens, "runner token service")
	assert.NotNil(t, svc.sessionActivator, "session activator")
	assert.NotNil(t, svc.actionLogs, "action log service")
	assert.NotNil(t, svc.webhooks, "webhook service")

	// Streaming is frozen (D1) and off by default, so its two handlers are the
	// only fields allowed to be nil here.
	assert.Nil(t, svc.streams, "streams handler without a stream manager")
	assert.Nil(t, svc.signaling, "signaling handler without a stream manager")
}

// TestAdminServices_OptionsCoverEveryField guards the other half of the drift:
// a service can be built and then never turned into an option. Counting fields
// against options catches a field added to the struct that options() forgot.
func TestAdminServices_OptionsCoverEveryField(t *testing.T) {
	svc := buildAdminServices(fullAdminDeps(t))
	// Streaming is off by default, so its two handlers never come out of
	// buildAdminServices here. options() only tests them for nil, so zero
	// values are enough to make the set complete.
	svc.streams = &admin.StreamsHandler{}
	svc.signaling = &admin.SignalingHandler{}

	assert.Equal(t,
		reflect.TypeOf(adminServices{}).NumField(),
		len(svc.options()),
		"every adminServices field must render an admin option",
	)
}

// TestBuildAdminServices_SkipsWhatIsGenuinelyAbsent: a server started with no
// database still serves health, and reports nothing else as available. The
// point is that options() must not hand a nil implementation to a WithX
// option, which would make the handler's "is this configured" guard pass on
// its way to a nil dereference.
func TestBuildAdminServices_SkipsWhatIsGenuinelyAbsent(t *testing.T) {
	svc := buildAdminServices(adminDeps{
		health: health.NewChecker(),
		logger: zap.NewNop(),
	})

	assert.NotNil(t, svc.health)
	assert.Nil(t, svc.apiKeys)
	assert.Nil(t, svc.providerConfigs)
	assert.Nil(t, svc.agentConfigs)
	assert.Nil(t, svc.runners)
	assert.Len(t, svc.options(), 1, "only the health service is available")
}

// TestBuildAdminServices_AgentConfigsNeedEncryption: an agent config is an API
// key with a name attached. Without a key to encrypt it under, the route stays
// unwired rather than writing the credential in plaintext - and the rest of
// the config surface stays available.
func TestBuildAdminServices_AgentConfigsNeedEncryption(t *testing.T) {
	deps := fullAdminDeps(t)
	deps.crypto = nil

	svc := buildAdminServices(deps)

	assert.Nil(t, svc.agentConfigs, "no encryption key means no agent config routes")
	assert.NotNil(t, svc.providerConfigs, "provider configs hold no secrets and stay available")
}

// TestStoreOrNil covers the nil-interface trap: a nil *postgres.Store assigned
// to a store.Store field is not nil, and every "is there a database" check
// downstream would read true.
func TestStoreOrNil(t *testing.T) {
	assert.Nil(t, storeOrNil(nil))
}

// stubEncryptor stands in for the envelope crypto service.
type stubEncryptor struct {
	err error
}

func (s stubEncryptor) EncryptString(_ context.Context, resourceType, resourceID, plaintext string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	// Encoded rather than concatenated: a stub that embeds the plaintext would
	// make "the raw key must not be stored" pass for the wrong reason.
	return resourceType + ":" + resourceID + ":" + base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
}

// sealed is what stubEncryptor produces, so the tests can assert on the exact
// value without restating the encoding.
func sealed(resourceID, plaintext string) string {
	return agentConfigResource + ":" + resourceID + ":" + base64.StdEncoding.EncodeToString([]byte(plaintext))
}

// fakeConfigStore is an in-memory stand-in for the two narrow config stores.
type fakeConfigStore struct {
	agents    map[string]*store.AgentConfig
	providers map[string]*store.ProviderConfig
}

func newFakeConfigStore() *fakeConfigStore {
	return &fakeConfigStore{
		agents:    map[string]*store.AgentConfig{},
		providers: map[string]*store.ProviderConfig{},
	}
}

func (f *fakeConfigStore) CreateAgentConfig(_ context.Context, c *store.AgentConfig) error {
	f.agents[c.ID] = c
	return nil
}

func (f *fakeConfigStore) GetAgentConfig(_ context.Context, cfgID string) (*store.AgentConfig, error) {
	c, ok := f.agents[cfgID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return c, nil
}

func (f *fakeConfigStore) ListAgentConfigs(_ context.Context, opts store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	var items []*store.AgentConfig
	for _, c := range f.agents {
		if opts.Agent != nil && c.Agent != *opts.Agent {
			continue
		}
		items = append(items, c)
	}
	return &store.ListResult[store.AgentConfig]{Items: items, TotalCount: int64(len(items))}, nil
}

func (f *fakeConfigStore) UpdateAgentConfig(_ context.Context, cfgID string, u store.AgentConfigUpdates) error {
	c, ok := f.agents[cfgID]
	if !ok {
		return store.ErrNotFound
	}
	if u.Name != nil {
		c.Name = *u.Name
	}
	if u.APIKeyEncrypted != nil {
		c.APIKeyEncrypted = *u.APIKeyEncrypted
	}
	if u.Model != nil {
		c.Model = u.Model
	}
	return nil
}

func (f *fakeConfigStore) DeleteAgentConfig(_ context.Context, cfgID string) error {
	delete(f.agents, cfgID)
	return nil
}

func (f *fakeConfigStore) CreateProviderConfig(_ context.Context, c *store.ProviderConfig) error {
	f.providers[c.ID] = c
	return nil
}

func (f *fakeConfigStore) GetProviderConfig(_ context.Context, cfgID string) (*store.ProviderConfig, error) {
	c, ok := f.providers[cfgID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return c, nil
}

func (f *fakeConfigStore) ListProviderConfigs(_ context.Context, opts store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	var items []*store.ProviderConfig
	for _, c := range f.providers {
		if opts.Provider != nil && c.Provider != *opts.Provider {
			continue
		}
		items = append(items, c)
	}
	return &store.ListResult[store.ProviderConfig]{Items: items, TotalCount: int64(len(items))}, nil
}

func (f *fakeConfigStore) UpdateProviderConfig(_ context.Context, cfgID string, u store.ProviderConfigUpdates) error {
	c, ok := f.providers[cfgID]
	if !ok {
		return store.ErrNotFound
	}
	if u.Name != nil {
		c.Name = *u.Name
	}
	if u.Config != nil {
		c.Config = u.Config
	}
	if u.IsDefault != nil {
		c.IsDefault = *u.IsDefault
	}
	return nil
}

func (f *fakeConfigStore) DeleteProviderConfig(_ context.Context, cfgID string) error {
	delete(f.providers, cfgID)
	return nil
}

// TestAgentConfigAdapter_EncryptsTheAPIKey is the property that matters most
// here: the plaintext key the operator posted must never reach the store.
func TestAgentConfigAdapter_EncryptsTheAPIKey(t *testing.T) {
	fake := newFakeConfigStore()
	adapter := newAgentConfigAdapter(fake, stubEncryptor{})
	ctx := context.Background()

	cfg, err := adapter.Create(ctx, admin.CreateAgentConfigOptions{
		Name:   "claude-prod",
		Agent:  "claude",
		APIKey: "sk-ant-secret",
		Model:  "claude-opus-5",
	})
	require.NoError(t, err)

	assert.NotEmpty(t, cfg.ID)
	assert.NotContains(t, cfg.APIKeyEncrypted, "sk-ant-secret", "the raw key must not be stored")
	assert.Equal(t, sealed(cfg.ID, "sk-ant-secret"), cfg.APIKeyEncrypted)
	require.NotNil(t, cfg.Model)
	assert.Equal(t, "claude-opus-5", *cfg.Model)

	// The JSON columns are NOT NULL and every reader unmarshals them blind.
	assert.JSONEq(t, "{}", string(cfg.Extra))
	assert.JSONEq(t, "{}", string(cfg.Labels))
	assert.JSONEq(t, "{}", string(cfg.Annotations))
}

// TestAgentConfigAdapter_ReEncryptsOnUpdate: rotating the key must go through
// the same path, not land in the column verbatim.
func TestAgentConfigAdapter_ReEncryptsOnUpdate(t *testing.T) {
	fake := newFakeConfigStore()
	adapter := newAgentConfigAdapter(fake, stubEncryptor{})
	ctx := context.Background()

	cfg, err := adapter.Create(ctx, admin.CreateAgentConfigOptions{Name: "n", Agent: "claude", APIKey: "old"})
	require.NoError(t, err)

	rotated := "new-secret"
	updated, err := adapter.Update(ctx, cfg.ID, admin.UpdateAgentConfigOptions{APIKey: &rotated})
	require.NoError(t, err)

	assert.Equal(t, sealed(cfg.ID, "new-secret"), updated.APIKeyEncrypted)
}

// TestAgentConfigAdapter_FailsClosedOnEncryptionError: a config whose key
// could not be encrypted must not be written at all.
func TestAgentConfigAdapter_FailsClosedOnEncryptionError(t *testing.T) {
	fake := newFakeConfigStore()
	adapter := newAgentConfigAdapter(fake, stubEncryptor{err: errors.New("no data key")})

	_, err := adapter.Create(context.Background(), admin.CreateAgentConfigOptions{
		Name: "n", Agent: "claude", APIKey: "secret",
	})
	require.Error(t, err)
	assert.Empty(t, fake.agents, "nothing may be persisted when encryption fails")
}

// TestAgentConfigAdapter_AllowsBYOKConfigWithoutKey: a config that carries no
// key at all is legitimate (BYOK sessions supply their own), and must not be
// rejected by the encryption path.
func TestAgentConfigAdapter_AllowsBYOKConfigWithoutKey(t *testing.T) {
	fake := newFakeConfigStore()
	adapter := newAgentConfigAdapter(fake, stubEncryptor{err: errors.New("must not be called")})

	cfg, err := adapter.Create(context.Background(), admin.CreateAgentConfigOptions{Name: "n", Agent: "claude"})
	require.NoError(t, err)
	assert.Empty(t, cfg.APIKeyEncrypted)
}

// TestProviderConfigAdapter_RoundTrip walks the CRUD the admin routes expose,
// which is the surface admin runner spawn depends on.
func TestProviderConfigAdapter_RoundTrip(t *testing.T) {
	fake := newFakeConfigStore()
	adapter := newProviderConfigAdapter(fake)
	ctx := context.Background()

	created, err := adapter.Create(ctx, admin.CreateProviderConfigOptions{
		Name:     "docker-local",
		Provider: "docker",
		Config:   map[string]any{"image": "ghcr.io/chunlea/marionette-runner:latest"},
		Labels:   map[string]string{"env": "dev"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.JSONEq(t, `{"image":"ghcr.io/chunlea/marionette-runner:latest"}`, string(created.Config))
	assert.JSONEq(t, `{"env":"dev"}`, string(created.Labels))
	// suspend_config is NOT NULL; an omitted one is an empty object.
	assert.JSONEq(t, "{}", string(created.SuspendConfig))

	got, err := adapter.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)

	provider := "docker"
	listed, err := adapter.List(ctx, admin.ListProviderConfigsOptions{Provider: provider})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	assert.Equal(t, created.ID, listed.Items[0].ID)

	isDefault := true
	updated, err := adapter.Update(ctx, created.ID, admin.UpdateProviderConfigOptions{
		IsDefault: &isDefault,
		Config:    &map[string]any{"image": "other"},
	})
	require.NoError(t, err)
	assert.True(t, updated.IsDefault)
	assert.JSONEq(t, `{"image":"other"}`, string(updated.Config))

	require.NoError(t, adapter.Delete(ctx, created.ID))
	_, err = adapter.Get(ctx, created.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// TestMarshalObject_NeverEmits null: the JSON columns are NOT NULL, so an
// absent map has to become an empty object rather than a nil RawMessage.
func TestMarshalObject_NeverEmitsNull(t *testing.T) {
	assert.JSONEq(t, "{}", string(marshalObject(nil)))
	assert.JSONEq(t, "{}", string(marshalLabels(nil)))

	// A value that cannot be marshalled falls back to the same empty object
	// rather than writing a broken column.
	assert.JSONEq(t, "{}", string(marshalObject(map[string]any{"bad": make(chan int)})))

	var decoded map[string]string
	require.NoError(t, json.Unmarshal(marshalLabels(map[string]string{"a": "b"}), &decoded))
	assert.Equal(t, map[string]string{"a": "b"}, decoded)
}
