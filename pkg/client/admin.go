package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AdminClient provides access to the Marionette Admin API.
type AdminClient interface {
	// API Keys
	CreateAPIKey(ctx context.Context, opts CreateAPIKeyOptions) (*APIKeyWithSecret, error)
	GetAPIKey(ctx context.Context, id string) (*APIKey, error)
	ListAPIKeys(ctx context.Context, opts ListAPIKeysOptions) (*ListResult[APIKey], error)
	RevokeAPIKey(ctx context.Context, id string, reason string) error

	// Agent Configs
	CreateAgentConfig(ctx context.Context, opts CreateAgentConfigOptions) (*AgentConfig, error)
	GetAgentConfig(ctx context.Context, id string) (*AgentConfig, error)
	ListAgentConfigs(ctx context.Context, opts ListAgentConfigsOptions) (*ListResult[AgentConfig], error)
	UpdateAgentConfig(ctx context.Context, id string, opts UpdateAgentConfigOptions) (*AgentConfig, error)
	DeleteAgentConfig(ctx context.Context, id string) error

	// Provider Configs
	CreateProviderConfig(ctx context.Context, opts CreateProviderConfigOptions) (*ProviderConfig, error)
	GetProviderConfig(ctx context.Context, id string) (*ProviderConfig, error)
	ListProviderConfigs(ctx context.Context, opts ListProviderConfigsOptions) (*ListResult[ProviderConfig], error)
	UpdateProviderConfig(ctx context.Context, id string, opts UpdateProviderConfigOptions) (*ProviderConfig, error)
	DeleteProviderConfig(ctx context.Context, id string) error

	// Runners (admin operations)
	SpawnRunner(ctx context.Context, opts SpawnRunnerOptions) (*AdminRunner, error)
	GetRunner(ctx context.Context, id string) (*AdminRunner, error)
	ListRunners(ctx context.Context, opts ListRunnersOptions) (*ListResult[AdminRunner], error)
	DestroyRunner(ctx context.Context, id string) error

	// Runner Tokens
	CreateRunnerToken(ctx context.Context, opts CreateRunnerTokenOptions) (*RunnerTokenWithSecret, error)
	GetRunnerToken(ctx context.Context, id string) (*RunnerToken, error)
	ListRunnerTokens(ctx context.Context, opts ListRunnerTokensOptions) (*ListResult[RunnerToken], error)
	RevokeRunnerToken(ctx context.Context, id string, reason string) error
	RotateRunnerToken(ctx context.Context, id string) (*RunnerTokenWithSecret, error)

	// Sessions (admin operations)
	ActivateSession(ctx context.Context, sessionID, runnerID string) error
	SuspendSession(ctx context.Context, sessionID, strategy string) error

	// Profiles
	CreateProfile(ctx context.Context, opts CreateProfileOptions) (*Profile, error)
	GetProfile(ctx context.Context, id string) (*Profile, error)
	ListProfiles(ctx context.Context, opts ListProfilesOptions) (*ListResult[Profile], error)
	UpdateProfile(ctx context.Context, id string, opts UpdateProfileOptions) (*Profile, error)
	DeleteProfile(ctx context.Context, id string) error
}

// APIKeyWithSecret includes the raw token, which the server returns only when
// the key is created.
//
// The admin API nests the key as {"key": {...}, "raw_token": "..."}, but the
// fields are embedded here so callers can reach them directly, exactly as
// RunnerTokenWithSecret does. Before the JSON methods below existed this type
// declared `Key string` against an object, so decoding failed outright with
// "cannot unmarshal object into Go struct field ... of type string" — the
// SDK's CreateAPIKey had never once worked.
type APIKeyWithSecret struct {
	APIKey
	RawToken string `json:"raw_token"`
}

// apiKeyWithSecretWire is the admin API's on-the-wire shape.
type apiKeyWithSecretWire struct {
	Key      *APIKey `json:"key"`
	RawToken string  `json:"raw_token"`
}

// UnmarshalJSON decodes the admin API's nested response into the flattened
// struct. A response without a "key" object is rejected rather than decoded
// into zero values.
func (k *APIKeyWithSecret) UnmarshalJSON(data []byte) error {
	var wire apiKeyWithSecretWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Key == nil {
		return fmt.Errorf("api key response has no %q object", "key")
	}

	k.APIKey = *wire.Key
	k.RawToken = wire.RawToken

	return nil
}

// MarshalJSON emits the same nested shape UnmarshalJSON accepts, so the type
// round-trips.
func (k APIKeyWithSecret) MarshalJSON() ([]byte, error) {
	key := k.APIKey
	return json.Marshal(apiKeyWithSecretWire{
		Key:      &key,
		RawToken: k.RawToken,
	})
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
	RunnerToken
	RawToken string `json:"raw_token"`
}

// runnerTokenWithSecretWire is the admin API's on-the-wire shape.
// It must stay in sync with admin.CreateRunnerTokenResponse and
// admin.RotateRunnerTokenResponse, which share this layout.
type runnerTokenWithSecretWire struct {
	Token    *RunnerToken `json:"token"`
	RawToken string       `json:"raw_token"`
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

// setAdminLabels writes a label filter in the format the admin API reads:
// one "labels" parameter holding comma-separated key=value pairs.
//
// This is not the public API's format. Every admin list method used to send
// the public one - labels[key]=value - which the admin handlers never look at,
// so the filter was silently dropped and the caller got an unfiltered page it
// had no way to tell apart from a correctly filtered one.
//
// Pairs are sorted so the query string is stable, which is what lets a test
// assert on it.
func setAdminLabels(params url.Values, labels map[string]string) {
	if len(labels) == 0 {
		return
	}

	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)

	params.Set("labels", strings.Join(pairs, ","))
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
func (c *HTTPAdminClient) GetAPIKey(ctx context.Context, id string) (*APIKey, error) {
	var result APIKey
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/keys/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListAPIKeys lists API keys with optional filtering.
func (c *HTTPAdminClient) ListAPIKeys(ctx context.Context, opts ListAPIKeysOptions) (*ListResult[APIKey], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	setAdminLabels(params, opts.Labels)

	path := "/admin/api/v1/keys"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[APIKey]
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
func (c *HTTPAdminClient) CreateAgentConfig(ctx context.Context, opts CreateAgentConfigOptions) (*AgentConfig, error) {
	var result AgentConfig
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/agent-configs", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAgentConfig retrieves an agent configuration by ID.
func (c *HTTPAdminClient) GetAgentConfig(ctx context.Context, id string) (*AgentConfig, error) {
	var result AgentConfig
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/agent-configs/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListAgentConfigs lists agent configurations with optional filtering.
func (c *HTTPAdminClient) ListAgentConfigs(ctx context.Context, opts ListAgentConfigsOptions) (*ListResult[AgentConfig], error) {
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
	setAdminLabels(params, opts.Labels)

	path := "/admin/api/v1/agent-configs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[AgentConfig]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateAgentConfig updates an agent configuration.
func (c *HTTPAdminClient) UpdateAgentConfig(ctx context.Context, id string, opts UpdateAgentConfigOptions) (*AgentConfig, error) {
	var result AgentConfig
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
func (c *HTTPAdminClient) CreateProviderConfig(ctx context.Context, opts CreateProviderConfigOptions) (*ProviderConfig, error) {
	var result ProviderConfig
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/provider-configs", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProviderConfig retrieves a provider configuration by ID.
func (c *HTTPAdminClient) GetProviderConfig(ctx context.Context, id string) (*ProviderConfig, error) {
	var result ProviderConfig
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/provider-configs/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListProviderConfigs lists provider configurations with optional filtering.
func (c *HTTPAdminClient) ListProviderConfigs(ctx context.Context, opts ListProviderConfigsOptions) (*ListResult[ProviderConfig], error) {
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
	setAdminLabels(params, opts.Labels)

	path := "/admin/api/v1/provider-configs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[ProviderConfig]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateProviderConfig updates a provider configuration.
func (c *HTTPAdminClient) UpdateProviderConfig(ctx context.Context, id string, opts UpdateProviderConfigOptions) (*ProviderConfig, error) {
	var result ProviderConfig
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
func (c *HTTPAdminClient) SpawnRunner(ctx context.Context, opts SpawnRunnerOptions) (*AdminRunner, error) {
	var result AdminRunner
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/runners/spawn", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetRunner retrieves a runner by ID, in the operator's view: unlike the
// public one it names the provider config and instance behind the runner.
func (c *HTTPAdminClient) GetRunner(ctx context.Context, id string) (*AdminRunner, error) {
	var result AdminRunner
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/runners/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListRunners lists runners with optional filtering.
func (c *HTTPAdminClient) ListRunners(ctx context.Context, opts ListRunnersOptions) (*ListResult[AdminRunner], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	// The admin API reads one comma-separated status parameter, not a
	// repeated one; the public API is the one that repeats.
	if len(opts.Status) > 0 {
		params.Set("status", strings.Join(opts.Status, ","))
	}
	if opts.PoolName != "" {
		params.Set("pool_name", opts.PoolName)
	}
	setAdminLabels(params, opts.Labels)

	path := "/admin/api/v1/runners"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[AdminRunner]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
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
func (c *HTTPAdminClient) GetRunnerToken(ctx context.Context, id string) (*RunnerToken, error) {
	var result RunnerToken
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/runner-tokens/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListRunnerTokens lists runner tokens with optional filtering.
func (c *HTTPAdminClient) ListRunnerTokens(ctx context.Context, opts ListRunnerTokensOptions) (*ListResult[RunnerToken], error) {
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
	setAdminLabels(params, opts.Labels)

	path := "/admin/api/v1/runner-tokens"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[RunnerToken]
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
func (c *HTTPAdminClient) CreateProfile(ctx context.Context, opts CreateProfileOptions) (*Profile, error) {
	var result Profile
	if err := c.doRequest(ctx, http.MethodPost, "/admin/api/v1/profiles", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProfile retrieves a profile by ID.
func (c *HTTPAdminClient) GetProfile(ctx context.Context, id string) (*Profile, error) {
	var result Profile
	if err := c.doRequest(ctx, http.MethodGet, "/admin/api/v1/profiles/"+id, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListProfiles lists profiles with optional filtering.
func (c *HTTPAdminClient) ListProfiles(ctx context.Context, opts ListProfilesOptions) (*ListResult[Profile], error) {
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
	setAdminLabels(params, opts.Labels)

	path := "/admin/api/v1/profiles"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[Profile]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateProfile updates a profile.
func (c *HTTPAdminClient) UpdateProfile(ctx context.Context, id string, opts UpdateProfileOptions) (*Profile, error) {
	var result Profile
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
