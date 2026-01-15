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
)

// HTTPClient implements the Client interface using HTTP requests.
type HTTPClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// HTTPClientOption is a functional option for configuring HTTPClient.
type HTTPClientOption func(*HTTPClient)

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(hc *http.Client) HTTPClientOption {
	return func(c *HTTPClient) {
		c.httpClient = hc
	}
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(timeout time.Duration) HTTPClientOption {
	return func(c *HTTPClient) {
		c.httpClient.Timeout = timeout
	}
}

// NewHTTPClient creates a new HTTP client for the Marionette API.
func NewHTTPClient(baseURL, apiKey string, opts ...HTTPClientOption) *HTTPClient {
	c := &HTTPClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// doRequest performs an HTTP request and unmarshals the response into v.
// The response body is always closed after this call.
func (c *HTTPClient) doRequest(ctx context.Context, method, path string, body, v any) error {
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

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
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
func (c *HTTPClient) parseError(resp *http.Response) error {
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

// Sessions

// CreateSession creates a new session.
func (c *HTTPClient) CreateSession(ctx context.Context, opts CreateSessionOptions) (*Session, error) {
	reqBody := map[string]any{
		"agent": opts.Agent,
	}
	if opts.Name != "" {
		reqBody["name"] = opts.Name
	}
	if opts.AgentConfigID != "" {
		reqBody["agent_config_id"] = opts.AgentConfigID
	}
	if opts.APIKey != "" {
		reqBody["api_key"] = opts.APIKey
	}
	if opts.LifecycleMode != "" {
		reqBody["lifecycle_mode"] = opts.LifecycleMode
	}
	if opts.IdleTimeoutSeconds > 0 {
		reqBody["idle_timeout_seconds"] = opts.IdleTimeoutSeconds
	}
	if len(opts.Labels) > 0 {
		reqBody["labels"] = opts.Labels
	}

	var session Session
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/sessions", reqBody, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

// GetSession retrieves a session by ID.
func (c *HTTPClient) GetSession(ctx context.Context, id string) (*Session, error) {
	var session Session
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/sessions/"+id, nil, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

// ListSessions lists sessions with optional filtering.
func (c *HTTPClient) ListSessions(ctx context.Context, opts ListSessionsOptions) (*ListResult[Session], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	for _, s := range opts.Status {
		params.Add("status", s)
	}
	if opts.Agent != "" {
		params.Set("agent", opts.Agent)
	}
	for k, v := range opts.Labels {
		params.Set("labels["+k+"]", v)
	}

	path := "/api/v1/sessions"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[Session]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SuspendSession suspends an active session.
func (c *HTTPClient) SuspendSession(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodPost, "/api/v1/sessions/"+id+"/suspend", nil, nil)
}

// ResumeSession resumes a suspended session.
func (c *HTTPClient) ResumeSession(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodPost, "/api/v1/sessions/"+id+"/resume", nil, nil)
}

// TerminateSession terminates a session.
func (c *HTTPClient) TerminateSession(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodDelete, "/api/v1/sessions/"+id, nil, nil)
}

// Tasks

// CreateTask creates a new task in a session.
func (c *HTTPClient) CreateTask(ctx context.Context, opts CreateTaskOptions) (*Task, error) {
	reqBody := map[string]any{
		"session_id": opts.SessionID,
		"prompt":     opts.Prompt,
	}
	if opts.ContinueFrom != "" {
		reqBody["continue_from"] = opts.ContinueFrom
	}
	if opts.TimeoutSeconds > 0 {
		reqBody["timeout_seconds"] = opts.TimeoutSeconds
	}
	if opts.MaxRetries > 0 {
		reqBody["max_retries"] = opts.MaxRetries
	}
	if len(opts.Labels) > 0 {
		reqBody["labels"] = opts.Labels
	}

	var task Task
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/tasks", reqBody, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// GetTask retrieves a task by ID.
func (c *HTTPClient) GetTask(ctx context.Context, id string) (*Task, error) {
	var task Task
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/tasks/"+id, nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ListTasks lists tasks with optional filtering.
func (c *HTTPClient) ListTasks(ctx context.Context, opts ListTasksOptions) (*ListResult[Task], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.SessionID != "" {
		params.Set("session_id", opts.SessionID)
	}
	for _, s := range opts.Status {
		params.Add("status", s)
	}

	path := "/api/v1/tasks"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[Task]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelTask cancels a pending or running task.
func (c *HTTPClient) CancelTask(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodPost, "/api/v1/tasks/"+id+"/cancel", nil, nil)
}

// GetTaskLogs retrieves logs for a task.
func (c *HTTPClient) GetTaskLogs(ctx context.Context, id string, opts GetLogsOptions) (LogIterator, error) {
	params := url.Values{}
	if opts.Tail > 0 {
		params.Set("limit", strconv.Itoa(opts.Tail))
	}
	if opts.SinceSequence > 0 {
		params.Set("since_sequence", strconv.FormatInt(opts.SinceSequence, 10))
	}

	path := "/api/v1/tasks/" + id + "/logs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result struct {
		Items      []*Log `json:"items"`
		TotalCount int64  `json:"total_count"`
		NextCursor string `json:"next_cursor,omitempty"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}

	return &sliceLogIterator{
		logs:  result.Items,
		index: 0,
	}, nil
}

// sliceLogIterator implements LogIterator for a slice of logs.
type sliceLogIterator struct {
	logs  []*Log
	index int
}

// Next returns the next log entry.
func (it *sliceLogIterator) Next() (*Log, error) {
	if it.index >= len(it.logs) {
		return nil, io.EOF
	}
	log := it.logs[it.index]
	it.index++
	return log, nil
}

// Close releases resources.
func (it *sliceLogIterator) Close() error {
	return nil
}

// Runners

// GetRunner retrieves a runner by ID.
func (c *HTTPClient) GetRunner(ctx context.Context, id string) (*Runner, error) {
	var runner Runner
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/runners/"+id, nil, &runner); err != nil {
		return nil, err
	}
	return &runner, nil
}

// ListRunners lists runners with optional filtering.
func (c *HTTPClient) ListRunners(ctx context.Context, opts ListRunnersOptions) (*ListResult[Runner], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	for _, s := range opts.Status {
		params.Add("status", s)
	}
	if opts.PoolName != "" {
		params.Set("pool_name", opts.PoolName)
	}
	for k, v := range opts.Labels {
		params.Set("labels["+k+"]", v)
	}

	path := "/api/v1/runners"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[Runner]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Permissions

// GetPermission retrieves a permission request by ID.
func (c *HTTPClient) GetPermission(ctx context.Context, id string) (*PermissionRequest, error) {
	var perm PermissionRequest
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/permissions/"+id, nil, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

// ListPermissions lists permission requests with optional filtering.
func (c *HTTPClient) ListPermissions(ctx context.Context, opts ListPermissionsOptions) (*ListResult[PermissionRequest], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.SessionID != "" {
		params.Set("session_id", opts.SessionID)
	}
	if opts.TaskID != "" {
		params.Set("task_id", opts.TaskID)
	}
	for _, s := range opts.Status {
		params.Add("status", s)
	}
	for _, r := range opts.RiskLevel {
		params.Add("risk_level", r)
	}

	path := "/api/v1/permissions"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[PermissionRequest]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ApprovePermission approves a pending permission request.
func (c *HTTPClient) ApprovePermission(ctx context.Context, id string, reason string) error {
	var reqBody any
	if reason != "" {
		reqBody = map[string]string{"reason": reason}
	}
	return c.doRequest(ctx, http.MethodPost, "/api/v1/permissions/"+id+"/approve", reqBody, nil)
}

// DenyPermission denies a pending permission request.
func (c *HTTPClient) DenyPermission(ctx context.Context, id string, reason string) error {
	var reqBody any
	if reason != "" {
		reqBody = map[string]string{"reason": reason}
	}
	return c.doRequest(ctx, http.MethodPost, "/api/v1/permissions/"+id+"/deny", reqBody, nil)
}

// Tunnels

// CreateTunnel creates a new tunnel for a session.
func (c *HTTPClient) CreateTunnel(ctx context.Context, opts CreateTunnelOptions) (*Tunnel, error) {
	reqBody := map[string]any{
		"type":       opts.Type,
		"local_port": opts.LocalPort,
		"public":     opts.Public,
	}

	var tunnel Tunnel
	path := "/api/v1/sessions/" + opts.SessionID + "/tunnels"
	if err := c.doRequest(ctx, http.MethodPost, path, reqBody, &tunnel); err != nil {
		return nil, err
	}
	return &tunnel, nil
}

// GetTunnel retrieves a tunnel by ID.
func (c *HTTPClient) GetTunnel(ctx context.Context, id string) (*Tunnel, error) {
	var tunnel Tunnel
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/tunnels/"+id, nil, &tunnel); err != nil {
		return nil, err
	}
	return &tunnel, nil
}

// ListTunnels lists tunnels for a session.
func (c *HTTPClient) ListTunnels(ctx context.Context, opts ListTunnelsOptions) (*ListResult[Tunnel], error) {
	path := "/api/v1/sessions/" + opts.SessionID + "/tunnels"

	var result ListResult[Tunnel]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CloseTunnel closes a tunnel.
func (c *HTTPClient) CloseTunnel(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodDelete, "/api/v1/tunnels/"+id, nil, nil)
}

// Scheduled Tasks

// CreateScheduledTask creates a new scheduled task.
func (c *HTTPClient) CreateScheduledTask(ctx context.Context, opts CreateScheduledTaskOptions) (*ScheduledTask, error) {
	reqBody := map[string]any{
		"session_id":      opts.SessionID,
		"name":            opts.Name,
		"cron_expression": opts.CronExpression,
		"prompt_template": opts.PromptTemplate,
	}
	if opts.Description != "" {
		reqBody["description"] = opts.Description
	}
	if opts.Timezone != "" {
		reqBody["timezone"] = opts.Timezone
	}
	if opts.TimeoutSeconds > 0 {
		reqBody["timeout_seconds"] = opts.TimeoutSeconds
	}
	if opts.MaxRetries > 0 {
		reqBody["max_retries"] = opts.MaxRetries
	}
	if opts.OnFailure != "" {
		reqBody["on_failure"] = opts.OnFailure
	}
	if opts.MaxConsecutiveFailures != nil {
		reqBody["max_consecutive_failures"] = *opts.MaxConsecutiveFailures
	}
	if len(opts.Labels) > 0 {
		reqBody["labels"] = opts.Labels
	}

	var task ScheduledTask
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/scheduled-tasks", reqBody, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// GetScheduledTask retrieves a scheduled task by ID.
func (c *HTTPClient) GetScheduledTask(ctx context.Context, id string) (*ScheduledTask, error) {
	var task ScheduledTask
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/scheduled-tasks/"+id, nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ListScheduledTasks lists scheduled tasks with optional filtering.
func (c *HTTPClient) ListScheduledTasks(ctx context.Context, opts ListScheduledTasksOptions) (*ListResult[ScheduledTask], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.SessionID != "" {
		params.Set("session_id", opts.SessionID)
	}
	for _, s := range opts.Status {
		params.Add("status", s)
	}

	path := "/api/v1/scheduled-tasks"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult[ScheduledTask]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateScheduledTask updates a scheduled task.
func (c *HTTPClient) UpdateScheduledTask(ctx context.Context, id string, opts UpdateScheduledTaskOptions) (*ScheduledTask, error) {
	reqBody := make(map[string]any)
	if opts.Name != nil {
		reqBody["name"] = *opts.Name
	}
	if opts.Description != nil {
		reqBody["description"] = *opts.Description
	}
	if opts.CronExpression != nil {
		reqBody["cron_expression"] = *opts.CronExpression
	}
	if opts.Timezone != nil {
		reqBody["timezone"] = *opts.Timezone
	}
	if opts.PromptTemplate != nil {
		reqBody["prompt_template"] = *opts.PromptTemplate
	}
	if opts.TimeoutSeconds != nil {
		reqBody["timeout_seconds"] = *opts.TimeoutSeconds
	}
	if opts.MaxRetries != nil {
		reqBody["max_retries"] = *opts.MaxRetries
	}
	if opts.OnFailure != nil {
		reqBody["on_failure"] = *opts.OnFailure
	}
	if opts.MaxConsecutiveFailures != nil {
		reqBody["max_consecutive_failures"] = *opts.MaxConsecutiveFailures
	}
	if opts.Labels != nil {
		reqBody["labels"] = opts.Labels
	}

	var task ScheduledTask
	if err := c.doRequest(ctx, http.MethodPatch, "/api/v1/scheduled-tasks/"+id, reqBody, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// DeleteScheduledTask deletes a scheduled task.
func (c *HTTPClient) DeleteScheduledTask(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodDelete, "/api/v1/scheduled-tasks/"+id, nil, nil)
}

// PauseScheduledTask pauses a scheduled task.
func (c *HTTPClient) PauseScheduledTask(ctx context.Context, id string) (*ScheduledTask, error) {
	var task ScheduledTask
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/scheduled-tasks/"+id+"/pause", nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ResumeScheduledTask resumes a paused scheduled task.
func (c *HTTPClient) ResumeScheduledTask(ctx context.Context, id string) (*ScheduledTask, error) {
	var task ScheduledTask
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/scheduled-tasks/"+id+"/resume", nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// TriggerScheduledTask manually triggers a scheduled task.
func (c *HTTPClient) TriggerScheduledTask(ctx context.Context, id string) (*Task, error) {
	var task Task
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/scheduled-tasks/"+id+"/trigger", nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// Ensure HTTPClient implements Client interface.
var _ Client = (*HTTPClient)(nil)
