// Package admintypes defines the wire contract of the Marionette admin API.
//
// It is the admin counterpart of apitypes, and follows the same two rules:
// adding a database column must not change the API, and no field is a raw JSON
// blob. There is a third rule here that matters more than either, because this
// is the surface that holds credentials:
//
//   - A secret leaves the server exactly once, in the response to the call
//     that created or rotated it, through a field that exists only on that
//     response. Nothing here carries a hash, an encrypted blob, or a
//     previous-token hash — not behind `json:"-"`, but absent, so a future
//     edit cannot expose one by forgetting a tag.
//
// The list envelope and the error body are shared with the public API rather
// than redeclared, so a client cannot need two ways to page or to read a
// failure.
package admintypes

import (
	"time"

	"github.com/chunlea/marionette/pkg/server/api/apitypes"
)

// ListResponse is the envelope every admin list endpoint returns.
//
// It is the public API's envelope: the admin API used to answer with its own,
// which omitted has_more, so no admin client could tell a full page from the
// last one.
type ListResponse[T any] = apitypes.ListResponse[T]

// ErrorResponse is the body of every non-2xx admin response.
type ErrorResponse = apitypes.ErrorResponse

// APIKey is a credential for the public API.
type APIKey struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// KeyPrefix is the readable head of the token, for telling keys apart.
	// The token itself is returned only by the create call.
	KeyPrefix    string            `json:"key_prefix"`
	Scopes       []string          `json:"scopes"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	CreatedAt    time.Time         `json:"created_at"`
	CreatedBy    *string           `json:"created_by,omitempty"`
	LastUsedAt   *time.Time        `json:"last_used_at,omitempty"`
	ExpiresAt    *time.Time        `json:"expires_at,omitempty"`
	RevokedAt    *time.Time        `json:"revoked_at,omitempty"`
	RevokeReason *string           `json:"revoke_reason,omitempty"`
}

// CreatedAPIKey is the response to creating an API key.
//
// RawToken is the only time the token is readable. The field name is
// load-bearing: scripts/smoke.sh bootstraps its credentials by reading it.
type CreatedAPIKey struct {
	Key      *APIKey `json:"key"`
	RawToken string  `json:"raw_token"`
}

// RunnerToken authenticates a pool runner when it joins.
type RunnerToken struct {
	ID string `json:"id"`
	// TokenPrefix is the readable head of the token; the token itself is
	// returned only when created or rotated.
	TokenPrefix string `json:"token_prefix"`
	// RunnerID is set once a runner has claimed the token.
	RunnerID *string `json:"runner_id,omitempty"`
	PoolName string  `json:"pool_name"`
	Status   string  `json:"status" enum:"active,rotating,revoked,expired"`
	// RotationDeadline is when a rotating token's predecessor stops working.
	RotationDeadline *time.Time        `json:"rotation_deadline,omitempty"`
	Labels           map[string]string `json:"labels"`
	CreatedAt        time.Time         `json:"created_at"`
	CreatedBy        *string           `json:"created_by,omitempty"`
	LastUsedAt       *time.Time        `json:"last_used_at,omitempty"`
	ExpiresAt        *time.Time        `json:"expires_at,omitempty"`
	RevokedAt        *time.Time        `json:"revoked_at,omitempty"`
	RevokeReason     *string           `json:"revoke_reason,omitempty"`
}

// CreatedRunnerToken is the response to creating or rotating a runner token.
//
// As with CreatedAPIKey, raw_token appears once and scripts/smoke.sh reads it.
type CreatedRunnerToken struct {
	Token    *RunnerToken `json:"token"`
	RawToken string       `json:"raw_token"`
}

// AgentConfig holds the credentials and settings for one AI agent.
type AgentConfig struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Agent string `json:"agent"`
	// The agent's API key is write-only: it is accepted on create and update,
	// encrypted at rest, and never returned.
	Model       *string           `json:"model,omitempty"`
	BaseURL     *string           `json:"base_url,omitempty"`
	Extra       map[string]any    `json:"extra"`
	IsDefault   bool              `json:"is_default"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ProviderConfig holds the settings for one runner provider.
type ProviderConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// Config and SuspendConfig are provider-specific and are stored and
	// returned as given. They can hold provider credentials; the admin API is
	// the only surface that returns them.
	Config        map[string]any    `json:"config"`
	SuspendConfig map[string]any    `json:"suspend_config"`
	IsDefault     bool              `json:"is_default"`
	Labels        map[string]string `json:"labels"`
	Annotations   map[string]string `json:"annotations"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// Profile is a reusable runner configuration template.
type Profile struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Description      *string          `json:"description,omitempty"`
	ProviderConfigID *string          `json:"provider_config_id,omitempty"`
	Resources        map[string]any   `json:"resources"`
	Network          map[string]any   `json:"network"`
	InitScript       *string          `json:"init_script,omitempty"`
	CleanupScript    *string          `json:"cleanup_script,omitempty"`
	Tunnels          []map[string]any `json:"tunnels"`
	Selector         map[string]any   `json:"selector"`
	// IsBuiltin marks a profile the server ships; those cannot be edited.
	IsBuiltin   bool              `json:"is_builtin"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Runner is the operator's view of an execution environment.
//
// It is deliberately not apitypes.Runner: this view adds the provider
// identifiers, which are an operator concern and are withheld from the public
// API.
type Runner struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Hostname           string            `json:"hostname"`
	Status             string            `json:"status" enum:"offline,idle,busy,paused"`
	Tainted            bool              `json:"tainted"`
	TaintReason        *string           `json:"taint_reason,omitempty"`
	SandboxMode        string            `json:"sandbox_mode" enum:"runner-is-sandbox,runner-creates-sandbox,none"`
	SandboxTypes       []string          `json:"sandbox_types"`
	ProviderConfigID   *string           `json:"provider_config_id,omitempty"`
	ProviderInstanceID *string           `json:"provider_instance_id,omitempty"`
	PoolName           *string           `json:"pool_name,omitempty"`
	ProfileID          *string           `json:"profile_id,omitempty"`
	Capabilities       []string          `json:"capabilities"`
	Labels             map[string]string `json:"labels"`
	Annotations        map[string]string `json:"annotations"`
	LastSeenAt         *time.Time        `json:"last_seen_at,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

// Webhook is a subscription to server events.
type Webhook struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
	// SecretPrefix identifies the signing secret without disclosing it. The
	// secret itself is returned only when created or rotated.
	SecretPrefix      string `json:"secret_prefix"`
	IsActive          bool   `json:"is_active"`
	MaxRetries        int    `json:"max_retries"`
	RetryDelaySeconds int    `json:"retry_delay_seconds"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
	// Headers are sent with every delivery, and may themselves carry a
	// credential for the receiving endpoint.
	Headers     map[string]string `json:"headers"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// CreatedWebhook is the response to creating a webhook.
type CreatedWebhook struct {
	Webhook *Webhook `json:"webhook"`
	// Secret signs the deliveries. Readable once, here.
	Secret string `json:"secret"`
}

// RotatedWebhookSecret is the response to rotating a webhook's secret.
type RotatedWebhookSecret struct {
	Secret       string `json:"secret"`
	SecretPrefix string `json:"secret_prefix"`
}

// WebhookEvent is one delivery of one event to one webhook.
type WebhookEvent struct {
	ID        string         `json:"id"`
	WebhookID string         `json:"webhook_id"`
	EventType string         `json:"event_type"`
	Payload   map[string]any `json:"payload"`
	Status    string         `json:"status" enum:"pending,delivered,failed,exhausted,canceled"`
	Attempts  int            `json:"attempts"`

	LastError      *string    `json:"last_error,omitempty"`
	LastStatusCode *int       `json:"last_status_code,omitempty"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ActionLog is one audited action.
type ActionLog struct {
	ID           string         `json:"id"`
	ActorType    string         `json:"actor_type" enum:"user,api_key,system,runner"`
	ActorID      *string        `json:"actor_id,omitempty"`
	ActorName    *string        `json:"actor_name,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	SessionID    *string        `json:"session_id,omitempty"`
	TaskID       *string        `json:"task_id,omitempty"`
	Details      map[string]any `json:"details"`
	IPAddress    *string        `json:"ip_address,omitempty"`
	UserAgent    *string        `json:"user_agent,omitempty"`
	Success      bool           `json:"success"`
	ErrorMessage *string        `json:"error_message,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

// Health is the body of the admin health endpoints.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Service string `json:"service"`
}

// ServiceStatus is one service's entry in the status endpoint.
type ServiceStatus struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Status  string `json:"status" enum:"ok,error,unknown"`
	Message string `json:"message,omitempty"`
}

// Status reports every service the process is running.
type Status struct {
	Services []ServiceStatus `json:"services"`
}

// Accepted is returned by endpoints that queue work rather than doing it.
type Accepted struct {
	Status string `json:"status"`
}
