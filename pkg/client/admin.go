package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// AdminClient provides access to the Marionette Admin API.
type AdminClient interface {
	// API Keys
	CreateAPIKey(ctx context.Context, opts CreateAPIKeyOptions) (*APIKeyWithSecret, error)
	GetAPIKey(ctx context.Context, id string) (*store.APIKey, error)
	ListAPIKeys(ctx context.Context, opts ListAPIKeysOptions) (*ListResult[store.APIKey], error)
	RevokeAPIKey(ctx context.Context, id string, reason string) error

	// Agent Configs
	CreateAgentConfig(ctx context.Context, opts CreateAgentConfigOptions) (*store.AgentConfig, error)
	GetAgentConfig(ctx context.Context, id string) (*store.AgentConfig, error)
	ListAgentConfigs(ctx context.Context, opts ListAgentConfigsOptions) (*ListResult[store.AgentConfig], error)
	UpdateAgentConfig(ctx context.Context, id string, opts UpdateAgentConfigOptions) (*store.AgentConfig, error)
	DeleteAgentConfig(ctx context.Context, id string) error

	// Provider Configs
	CreateProviderConfig(ctx context.Context, opts CreateProviderConfigOptions) (*store.ProviderConfig, error)
	GetProviderConfig(ctx context.Context, id string) (*store.ProviderConfig, error)
	ListProviderConfigs(ctx context.Context, opts ListProviderConfigsOptions) (*ListResult[store.ProviderConfig], error)
	UpdateProviderConfig(ctx context.Context, id string, opts UpdateProviderConfigOptions) (*store.ProviderConfig, error)
	DeleteProviderConfig(ctx context.Context, id string) error

	// Runners (admin operations)
	SpawnRunner(ctx context.Context, opts SpawnRunnerOptions) (*store.Runner, error)
	DestroyRunner(ctx context.Context, id string) error

	// Runner Tokens
	CreateRunnerToken(ctx context.Context, opts CreateRunnerTokenOptions) (*RunnerTokenWithSecret, error)
	GetRunnerToken(ctx context.Context, id string) (*store.RunnerToken, error)
	ListRunnerTokens(ctx context.Context, opts ListRunnerTokensOptions) (*ListResult[store.RunnerToken], error)
	RevokeRunnerToken(ctx context.Context, id string, reason string) error
	RotateRunnerToken(ctx context.Context, id string) (*RunnerTokenWithSecret, error)

	// Sessions (admin operations)
	ActivateSession(ctx context.Context, sessionID, runnerID string) error
	SuspendSession(ctx context.Context, sessionID, strategy string) error

	// Profiles
	CreateProfile(ctx context.Context, opts CreateProfileOptions) (*store.Profile, error)
	GetProfile(ctx context.Context, id string) (*store.Profile, error)
	ListProfiles(ctx context.Context, opts ListProfilesOptions) (*ListResult[store.Profile], error)
	UpdateProfile(ctx context.Context, id string, opts UpdateProfileOptions) (*store.Profile, error)
	DeleteProfile(ctx context.Context, id string) error
}

// APIKeyWithSecret includes the raw API key (only returned on creation).
type APIKeyWithSecret struct {
	store.APIKey
	Key string `json:"key"`
}

// CreateAPIKeyOptions contains options for creating an API key.
type CreateAPIKeyOptions struct {
	Name   string            `json:"name"`
	Scopes []string          `json:"scopes,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// ListAPIKeysOptions contains options for listing API keys.
type ListAPIKeysOptions struct {
	Limit  int               `json:"limit,omitempty"`
	Cursor string            `json:"cursor,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// CreateAgentConfigOptions contains options for creating an agent config.
type CreateAgentConfigOptions struct {
	Name      string            `json:"name"`
	Agent     string            `json:"agent"`
	APIKey    string            `json:"api_key"`
	Model     string            `json:"model,omitempty"`
	BaseURL   string            `json:"base_url,omitempty"`
	IsDefault bool              `json:"is_default,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// ListAgentConfigsOptions contains options for listing agent configs.
type ListAgentConfigsOptions struct {
	Limit  int               `json:"limit,omitempty"`
	Cursor string            `json:"cursor,omitempty"`
	Agent  string            `json:"agent,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// UpdateAgentConfigOptions contains options for updating an agent config.
type UpdateAgentConfigOptions struct {
	Name      *string            `json:"name,omitempty"`
	APIKey    *string            `json:"api_key,omitempty"`
	Model     *string            `json:"model,omitempty"`
	BaseURL   *string            `json:"base_url,omitempty"`
	IsDefault *bool              `json:"is_default,omitempty"`
	Labels    *map[string]string `json:"labels,omitempty"`
}

// CreateProviderConfigOptions contains options for creating a provider config.
type CreateProviderConfigOptions struct {
	Name          string            `json:"name"`
	Provider      string            `json:"provider"`
	Config        map[string]any    `json:"config,omitempty"`
	SuspendConfig map[string]any    `json:"suspend_config,omitempty"`
	IsDefault     bool              `json:"is_default,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// ListProviderConfigsOptions contains options for listing provider configs.
type ListProviderConfigsOptions struct {
	Limit    int               `json:"limit,omitempty"`
	Cursor   string            `json:"cursor,omitempty"`
	Provider string            `json:"provider,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// UpdateProviderConfigOptions contains options for updating a provider config.
type UpdateProviderConfigOptions struct {
	Name          *string            `json:"name,omitempty"`
	Config        *map[string]any    `json:"config,omitempty"`
	SuspendConfig *map[string]any    `json:"suspend_config,omitempty"`
	IsDefault     *bool              `json:"is_default,omitempty"`
	Labels        *map[string]string `json:"labels,omitempty"`
}

// SpawnRunnerOptions contains options for spawning a runner.
type SpawnRunnerOptions struct {
	Name             string            `json:"name,omitempty"`
	ProviderConfigID string            `json:"provider_config_id,omitempty"`
	ProfileID        string            `json:"profile_id,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

// RunnerTokenWithSecret includes the raw token (only returned on creation/rotation).
//
// The admin API nests the token as {"token": {...}, "raw_token": "..."}, but
// the fields are embedded here so callers can reach them directly. The JSON
// methods below bridge the two shapes. Without them the embedded fields all
// decoded as their zero values, which is why `mctl admin runner-tokens create`
// printed a blank ID, pool and prefix while still showing the raw token.
type RunnerTokenWithSecret struct {
	store.RunnerToken
	RawToken string `json:"raw_token"`
}

// runnerTokenWithSecretWire is the admin API's on-the-wire shape.
// It must stay in sync with admin.CreateRunnerTokenResponse and
// admin.RotateRunnerTokenResponse, which share this layout.
type runnerTokenWithSecretWire struct {
	Token    *store.RunnerToken `json:"token"`
	RawToken string             `json:"raw_token"`
}

// UnmarshalJSON decodes the admin API's nested response into the flattened
// struct. A response without a "token" object is rejected rather than decoded
// into zero values: silent blanks are the failure this method exists to fix.
func (r *RunnerTokenWithSecret) UnmarshalJSON(data []byte) error {
	var wire runnerTokenWithSecretWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Token == nil {
		return fmt.Errorf("runner token response has no %q object", "token")
	}

	r.RunnerToken = *wire.Token
	r.RawToken = wire.RawToken

	return nil
}

// MarshalJSON emits the same nested shape UnmarshalJSON accepts, so the type
// round-trips.
func (r RunnerTokenWithSecret) MarshalJSON() ([]byte, error) {
	token := r.RunnerToken
	return json.Marshal(runnerTokenWithSecretWire{
		Token:    &token,
		RawToken: r.RawToken,
	})
}

// CreateRunnerTokenOptions contains options for creating a runner token.
type CreateRunnerTokenOptions struct {
	PoolName  string            `json:"pool_name"`
	Labels    map[string]string `json:"labels,omitempty"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
}

// ListRunnerTokensOptions contains options for listing runner tokens.
type ListRunnerTokensOptions struct {
	Limit          int               `json:"limit,omitempty"`
	Cursor         string            `json:"cursor,omitempty"`
	PoolName       string            `json:"pool_name,omitempty"`
	Status         []string          `json:"status,omitempty"`
	IncludeRevoked bool              `json:"include_revoked,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// CreateProfileOptions contains options for creating a profile.
type CreateProfileOptions struct {
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	ProviderConfigID string            `json:"provider_config_id,omitempty"`
	Resources        map[string]any    `json:"resources,omitempty"`
	Network          map[string]any    `json:"network,omitempty"`
	InitScript       string            `json:"init_script,omitempty"`
	CleanupScript    string            `json:"cleanup_script,omitempty"`
	Tunnels          []map[string]any  `json:"tunnels,omitempty"`
	Selector         map[string]any    `json:"selector,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Annotations      map[string]string `json:"annotations,omitempty"`
}

// ListProfilesOptions contains options for listing profiles.
type ListProfilesOptions struct {
	Limit            int               `json:"limit,omitempty"`
	Cursor           string            `json:"cursor,omitempty"`
	ProviderConfigID string            `json:"provider_config_id,omitempty"`
	IncludeBuiltin   bool              `json:"include_builtin,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

// UpdateProfileOptions contains options for updating a profile.
type UpdateProfileOptions struct {
	Name             *string            `json:"name,omitempty"`
	Description      *string            `json:"description,omitempty"`
	ProviderConfigID *string            `json:"provider_config_id,omitempty"`
	Resources        *map[string]any    `json:"resources,omitempty"`
	Network          *map[string]any    `json:"network,omitempty"`
	InitScript       *string            `json:"init_script,omitempty"`
	CleanupScript    *string            `json:"cleanup_script,omitempty"`
	Tunnels          *[]map[string]any  `json:"tunnels,omitempty"`
	Selector         *map[string]any    `json:"selector,omitempty"`
	Labels           *map[string]string `json:"labels,omitempty"`
	Annotations      *map[string]string `json:"annotations,omitempty"`
}

// HTTPAdminClient implements the AdminClient interface using HTTP requests.
type HTTPAdminClient struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// NewHTTPAdminClient creates a new HTTP admin client.
func NewHTTPAdminClient(baseURL, username, password string, opts ...HTTPClientOption) *HTTPAdminClient {
	c := &HTTPAdminClient{
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Apply options (reuse HTTPClientOption for http.Client config)
	dummyClient := &HTTPClient{httpClient: c.httpClient}
	for _, opt := range opts {
		opt(dummyClient)
	}
	c.httpClient = dummyClient.httpClient

	return c
}

// doRequest performs an HTTP request and unmarshals the response into v.
func (c *HTTPAdminClient) doRequest(ctx context.Context, method, path string, body, v any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}

	if v == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

// parseError parses an error response from the API.
func (c *HTTPAdminClient) parseError(resp *http.Response) error {
	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    resp.Status,
		}
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Code:       errResp.Code,
		Message:    errResp.Message,
	}
}

// API Keys

// CreateAPIKey creates a new API key.
func (c *HTTPAdminClient) CreateAPIKey(ctx context.Context, opts CreateAPIKeyOptions) (*APIKeyWithSecret, error) {
	var result APIKeyWithSecret
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/keys", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAPIKey retrieves an API key by ID.
func (c *HTTPAdminClient) GetAPIKey(ctx context.Context, id string) (*store.APIKey, error) {
	var result store.APIKey
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/keys/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListAPIKeys lists API keys with optional filtering.
func (c *HTTPAdminClient) ListAPIKeys(ctx context.Context, opts ListAPIKeysOptions) (*ListResult[store.APIKey], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	for k, v := range opts.Labels {
		params.Set("labels["+k+"]", v)
	}

	path := "/admin/api/v1/keys"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[store.APIKey]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RevokeAPIKey revokes an API key.
func (c *HTTPAdminClient) RevokeAPIKey(ctx context.Context, id string, reason string) error {
	var body any
	if reason != "" {
		body = map[string]string{"reason": reason}
	}
	return c.doRequest(ctx, http.MethodDelete, "/admin/api/v1/keys/"+id, body, nil)
}

// Agent Configs

// CreateAgentConfig creates a new agent configuration.
func (c *HTTPAdminClient) CreateAgentConfig(ctx context.Context, opts CreateAgentConfigOptions) (*store.AgentConfig, error) {
	var result store.AgentConfig
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/agent-configs", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAgentConfig retrieves an agent configuration by ID.
func (c *HTTPAdminClient) GetAgentConfig(ctx context.Context, id string) (*store.AgentConfig, error) {
	var result store.AgentConfig
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/agent-configs/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListAgentConfigs lists agent configurations with optional filtering.
func (c *HTTPAdminClient) ListAgentConfigs(ctx context.Context, opts ListAgentConfigsOptions) (*ListResult[store.AgentConfig], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.Agent != "" {
		params.Set("agent", opts.Agent)
	}
	for k, v := range opts.Labels {
		params.Set("labels["+k+"]", v)
	}

	path := "/admin/api/v1/agent-configs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[store.AgentConfig]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateAgentConfig updates an agent configuration.
func (c *HTTPAdminClient) UpdateAgentConfig(ctx context.Context, id string, opts UpdateAgentConfigOptions) (*store.AgentConfig, error) {
	var result store.AgentConfig
	if err := c.doRequest(ctx, http.MethodPut, "/admin/api/v1/agent-configs/"+id, opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteAgentConfig deletes an agent configuration.
func (c *HTTPAdminClient) DeleteAgentConfig(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodDelete, "/admin/api/v1/agent-configs/"+id, nil, nil)
}

// Provider Configs

// CreateProviderConfig creates a new provider configuration.
func (c *HTTPAdminClient) CreateProviderConfig(ctx context.Context, opts CreateProviderConfigOptions) (*store.ProviderConfig, error) {
	var result store.ProviderConfig
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/provider-configs", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProviderConfig retrieves a provider configuration by ID.
func (c *HTTPAdminClient) GetProviderConfig(ctx context.Context, id string) (*store.ProviderConfig, error) {
	var result store.ProviderConfig
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/provider-configs/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListProviderConfigs lists provider configurations with optional filtering.
func (c *HTTPAdminClient) ListProviderConfigs(ctx context.Context, opts ListProviderConfigsOptions) (*ListResult[store.ProviderConfig], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.Provider != "" {
		params.Set("provider", opts.Provider)
	}
	for k, v := range opts.Labels {
		params.Set("labels["+k+"]", v)
	}

	path := "/admin/api/v1/provider-configs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[store.ProviderConfig]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateProviderConfig updates a provider configuration.
func (c *HTTPAdminClient) UpdateProviderConfig(ctx context.Context, id string, opts UpdateProviderConfigOptions) (*store.ProviderConfig, error) {
	var result store.ProviderConfig
	if err := c.doRequest(ctx, http.MethodPut, "/admin/api/v1/provider-configs/"+id, opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteProviderConfig deletes a provider configuration.
func (c *HTTPAdminClient) DeleteProviderConfig(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodDelete, "/admin/api/v1/provider-configs/"+id, nil, nil)
}

// Runners (admin operations)

// SpawnRunner spawns a new runner.
func (c *HTTPAdminClient) SpawnRunner(ctx context.Context, opts SpawnRunnerOptions) (*store.Runner, error) {
	var result store.Runner
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/runners/spawn", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DestroyRunner destroys a runner.
func (c *HTTPAdminClient) DestroyRunner(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodDelete, "/admin/api/v1/runners/"+id, nil, nil)
}

// Sessions (admin operations)

// ActivateSession activates a session by attaching a runner to it.
func (c *HTTPAdminClient) ActivateSession(ctx context.Context, sessionID, runnerID string) error {
	body := map[string]string{"runner_id": runnerID}
	return c.doRequest(ctx, http.MethodPost, "/admin/api/v1/sessions/"+sessionID+"/activate", body, nil)
}

// SuspendSession suspends a session with the given strategy.
func (c *HTTPAdminClient) SuspendSession(ctx context.Context, sessionID, strategy string) error {
	var body any
	if strategy != "" {
		body = map[string]string{"strategy": strategy}
	}
	return c.doRequest(ctx, http.MethodPost, "/admin/api/v1/sessions/"+sessionID+"/suspend", body, nil)
}

// Runner Tokens

// CreateRunnerToken creates a new runner token.
func (c *HTTPAdminClient) CreateRunnerToken(ctx context.Context, opts CreateRunnerTokenOptions) (*RunnerTokenWithSecret, error) {
	var result RunnerTokenWithSecret
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/runner-tokens", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetRunnerToken retrieves a runner token by ID.
func (c *HTTPAdminClient) GetRunnerToken(ctx context.Context, id string) (*store.RunnerToken, error) {
	var result store.RunnerToken
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/runner-tokens/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListRunnerTokens lists runner tokens with optional filtering.
func (c *HTTPAdminClient) ListRunnerTokens(ctx context.Context, opts ListRunnerTokensOptions) (*ListResult[store.RunnerToken], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.PoolName != "" {
		params.Set("pool_name", opts.PoolName)
	}
	if len(opts.Status) > 0 {
		params.Set("status", strings.Join(opts.Status, ","))
	}
	if opts.IncludeRevoked {
		params.Set("include_revoked", "true")
	}
	for k, v := range opts.Labels {
		params.Set("labels["+k+"]", v)
	}

	path := "/admin/api/v1/runner-tokens"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[store.RunnerToken]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RevokeRunnerToken revokes a runner token.
func (c *HTTPAdminClient) RevokeRunnerToken(ctx context.Context, id string, reason string) error {
	var body any
	if reason != "" {
		body = map[string]string{"reason": reason}
	}
	return c.doRequest(ctx, http.MethodDelete, "/admin/api/v1/runner-tokens/"+id, body, nil)
}

// RotateRunnerToken rotates a runner token and returns the new token.
func (c *HTTPAdminClient) RotateRunnerToken(ctx context.Context, id string) (*RunnerTokenWithSecret, error) {
	var result RunnerTokenWithSecret
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/runner-tokens/"+id+"/rotate", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Profiles

// CreateProfile creates a new profile.
func (c *HTTPAdminClient) CreateProfile(ctx context.Context, opts CreateProfileOptions) (*store.Profile, error) {
	var result store.Profile
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/profiles", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProfile retrieves a profile by ID.
func (c *HTTPAdminClient) GetProfile(ctx context.Context, id string) (*store.Profile, error) {
	var result store.Profile
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/profiles/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListProfiles lists profiles with optional filtering.
func (c *HTTPAdminClient) ListProfiles(ctx context.Context, opts ListProfilesOptions) (*ListResult[store.Profile], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.ProviderConfigID != "" {
		params.Set("provider_config_id", opts.ProviderConfigID)
	}
	if opts.IncludeBuiltin {
		params.Set("include_builtin", "true")
	}
	for k, v := range opts.Labels {
		params.Set("labels["+k+"]", v)
	}

	path := "/admin/api/v1/profiles"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[store.Profile]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateProfile updates a profile.
func (c *HTTPAdminClient) UpdateProfile(ctx context.Context, id string, opts UpdateProfileOptions) (*store.Profile, error) {
	var result store.Profile
	if err := c.doRequest(ctx, http.MethodPut, "/admin/api/v1/profiles/"+id, opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteProfile deletes a profile.
func (c *HTTPAdminClient) DeleteProfile(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodDelete, "/admin/api/v1/profiles/"+id, nil, nil)
}

// Ensure HTTPAdminClient implements AdminClient interface.
var _ AdminClient = (*HTTPAdminClient)(nil)
