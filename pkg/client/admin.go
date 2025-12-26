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

// Ensure HTTPAdminClient implements AdminClient interface.
var _ AdminClient = (*HTTPAdminClient)(nil)
