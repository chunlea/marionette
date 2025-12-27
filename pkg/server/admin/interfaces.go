package admin

import (
	"context"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// APIKeyService defines the interface for API key management.
type APIKeyService interface {
	// Create creates a new API key and returns the key and plaintext token.
	Create(ctx context.Context, opts CreateAPIKeyOptions) (*store.APIKey, string, error)

	// Get retrieves an API key by ID.
	Get(ctx context.Context, id string) (*store.APIKey, error)

	// List returns API keys matching the given options.
	List(ctx context.Context, opts ListAPIKeysOptions) (*ListResult[store.APIKey], error)

	// Revoke revokes an API key with an optional reason.
	Revoke(ctx context.Context, id, reason string) error
}

// CreateAPIKeyOptions defines options for creating an API key.
type CreateAPIKeyOptions struct {
	Name        string            `json:"name"`
	Scopes      []string          `json:"scopes,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
}

// ListAPIKeysOptions defines options for listing API keys.
type ListAPIKeysOptions struct {
	Limit  int               `json:"limit,omitempty"`
	Cursor string            `json:"cursor,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// AgentConfigService defines the interface for agent configuration management.
type AgentConfigService interface {
	// Create creates a new agent configuration.
	Create(ctx context.Context, opts CreateAgentConfigOptions) (*store.AgentConfig, error)

	// Get retrieves an agent configuration by ID.
	Get(ctx context.Context, id string) (*store.AgentConfig, error)

	// List returns agent configurations matching the given options.
	List(ctx context.Context, opts ListAgentConfigsOptions) (*ListResult[store.AgentConfig], error)

	// Update updates an agent configuration.
	Update(ctx context.Context, id string, opts UpdateAgentConfigOptions) (*store.AgentConfig, error)

	// Delete deletes an agent configuration.
	Delete(ctx context.Context, id string) error
}

// CreateAgentConfigOptions defines options for creating an agent configuration.
type CreateAgentConfigOptions struct {
	Name      string            `json:"name"`
	Agent     string            `json:"agent"`
	APIKey    string            `json:"api_key"`
	Model     string            `json:"model,omitempty"`
	BaseURL   string            `json:"base_url,omitempty"`
	Extra     map[string]any    `json:"extra,omitempty"`
	IsDefault bool              `json:"is_default,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// UpdateAgentConfigOptions defines options for updating an agent configuration.
type UpdateAgentConfigOptions struct {
	Name      *string            `json:"name,omitempty"`
	APIKey    *string            `json:"api_key,omitempty"`
	Model     *string            `json:"model,omitempty"`
	BaseURL   *string            `json:"base_url,omitempty"`
	Extra     *map[string]any    `json:"extra,omitempty"`
	IsDefault *bool              `json:"is_default,omitempty"`
	Labels    *map[string]string `json:"labels,omitempty"`
}

// ListAgentConfigsOptions defines options for listing agent configurations.
type ListAgentConfigsOptions struct {
	Limit  int               `json:"limit,omitempty"`
	Cursor string            `json:"cursor,omitempty"`
	Agent  string            `json:"agent,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// ProviderConfigService defines the interface for provider configuration management.
type ProviderConfigService interface {
	// Create creates a new provider configuration.
	Create(ctx context.Context, opts CreateProviderConfigOptions) (*store.ProviderConfig, error)

	// Get retrieves a provider configuration by ID.
	Get(ctx context.Context, id string) (*store.ProviderConfig, error)

	// List returns provider configurations matching the given options.
	List(ctx context.Context, opts ListProviderConfigsOptions) (*ListResult[store.ProviderConfig], error)

	// Update updates a provider configuration.
	Update(ctx context.Context, id string, opts UpdateProviderConfigOptions) (*store.ProviderConfig, error)

	// Delete deletes a provider configuration.
	Delete(ctx context.Context, id string) error
}

// CreateProviderConfigOptions defines options for creating a provider configuration.
type CreateProviderConfigOptions struct {
	Name          string            `json:"name"`
	Provider      string            `json:"provider"`
	Config        map[string]any    `json:"config,omitempty"`
	SuspendConfig map[string]any    `json:"suspend_config,omitempty"`
	IsDefault     bool              `json:"is_default,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// UpdateProviderConfigOptions defines options for updating a provider configuration.
type UpdateProviderConfigOptions struct {
	Name          *string            `json:"name,omitempty"`
	Config        *map[string]any    `json:"config,omitempty"`
	SuspendConfig *map[string]any    `json:"suspend_config,omitempty"`
	IsDefault     *bool              `json:"is_default,omitempty"`
	Labels        *map[string]string `json:"labels,omitempty"`
}

// ListProviderConfigsOptions defines options for listing provider configurations.
type ListProviderConfigsOptions struct {
	Limit    int               `json:"limit,omitempty"`
	Cursor   string            `json:"cursor,omitempty"`
	Provider string            `json:"provider,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// RunnerAdminService defines the interface for runner administrative operations.
type RunnerAdminService interface {
	// Spawn creates a new runner using the specified provider.
	Spawn(ctx context.Context, opts SpawnRunnerOptions) (*store.Runner, error)

	// Destroy terminates and removes a runner.
	Destroy(ctx context.Context, id string) error

	// Get retrieves a runner by ID.
	Get(ctx context.Context, id string) (*store.Runner, error)

	// List returns runners matching the given options.
	List(ctx context.Context, opts ListRunnersOptions) (*ListResult[store.Runner], error)
}

// SpawnRunnerOptions defines options for spawning a runner.
type SpawnRunnerOptions struct {
	Name             string            `json:"name,omitempty"`
	ProviderConfigID string            `json:"provider_config_id,omitempty"`
	Provider         string            `json:"provider,omitempty"`
	ProfileID        string            `json:"profile_id,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

// ListRunnersOptions defines options for listing runners.
type ListRunnersOptions struct {
	Limit    int               `json:"limit,omitempty"`
	Cursor   string            `json:"cursor,omitempty"`
	Status   []string          `json:"status,omitempty"`
	PoolName string            `json:"pool_name,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// ListResult is a generic paginated list result.
type ListResult[T any] struct {
	Items      []*T   `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	TotalCount int64  `json:"total_count,omitempty"`
}

// SessionActivator defines the interface for activating sessions (for testing).
type SessionActivator interface {
	// Activate activates a session with the given runner.
	Activate(ctx context.Context, sessionID, runnerID string) error

	// Suspend suspends a session with the given strategy.
	Suspend(ctx context.Context, sessionID, strategy string) error
}

// TaskDispatcher defines the interface for dispatching tasks (for testing).
type TaskDispatcher interface {
	// Dispatch sends a task to its session's runner for execution.
	Dispatch(ctx context.Context, taskID string) error
}
