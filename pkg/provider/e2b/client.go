package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the E2B REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// NewClient creates a new E2B API client.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		baseURL: baseURL,
		apiKey:  apiKey,
	}
}

// Sandbox represents an E2B sandbox instance.
type Sandbox struct {
	SandboxID  string            `json:"sandboxID"`
	TemplateID string            `json:"templateID,omitempty"`
	Alias      string            `json:"alias,omitempty"`
	ClientID   string            `json:"clientID,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	StartedAt  time.Time         `json:"startedAt"`
	EndedAt    *time.Time        `json:"endedAt,omitempty"`
}

// CreateSandboxRequest is the request body for creating a sandbox.
type CreateSandboxRequest struct {
	TemplateID string            `json:"templateID"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Timeout    int               `json:"timeout,omitempty"` // timeout in seconds
	EnvVars    map[string]string `json:"envVars,omitempty"`
}

// CreateSandboxResponse is the response from creating a sandbox.
type CreateSandboxResponse struct {
	SandboxID  string `json:"sandboxID"`
	TemplateID string `json:"templateID"`
	ClientID   string `json:"clientID"`
}

// SetTimeoutRequest is the request body for setting sandbox timeout.
type SetTimeoutRequest struct {
	Timeout int `json:"timeout"` // timeout in seconds
}

// PauseSandboxResponse is the response from pausing a sandbox.
//
// The live API returns "sandboxID", matching every other response in this
// file. Go's JSON decoding is case-insensitive so the old "sandboxId" tag
// worked, but it described the API wrongly.
type PauseSandboxResponse struct {
	SandboxID string `json:"sandboxID"`
}

// APIError represents an error response from the E2B API.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("e2b api error %d: %s", e.Code, e.Message)
}

// CreateSandbox creates a new E2B sandbox.
func (c *Client) CreateSandbox(ctx context.Context, req *CreateSandboxRequest) (*CreateSandboxResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sandboxes", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	var result CreateSandboxResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

// GetSandbox retrieves information about a sandbox.
func (c *Client) GetSandbox(ctx context.Context, sandboxID string) (*Sandbox, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/sandboxes/"+sandboxID, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	var result Sandbox
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

// ListSandboxes returns all running sandboxes.
func (c *Client) ListSandboxes(ctx context.Context) ([]Sandbox, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/sandboxes", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	var result []Sandbox
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return result, nil
}

// KillSandbox terminates a sandbox.
func (c *Client) KillSandbox(ctx context.Context, sandboxID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/sandboxes/"+sandboxID, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := c.checkResponse(resp); err != nil {
		return err
	}

	return nil
}

// SetTimeout updates the sandbox timeout.
func (c *Client) SetTimeout(ctx context.Context, sandboxID string, timeoutSeconds int) error {
	body, err := json.Marshal(&SetTimeoutRequest{Timeout: timeoutSeconds})
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sandboxes/"+sandboxID+"/timeout", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := c.checkResponse(resp); err != nil {
		return err
	}

	return nil
}

// PauseSandbox pauses a sandbox (beta feature).
// Paused sandboxes preserve memory state and can be resumed.
func (c *Client) PauseSandbox(ctx context.Context, sandboxID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sandboxes/"+sandboxID+"/pause", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := c.checkResponse(resp); err != nil {
		return err
	}

	return nil
}

// ResumeSandboxRequest is the request body for resuming a sandbox.
type ResumeSandboxRequest struct {
	Timeout int `json:"timeout"` // timeout in seconds
}

// ResumeSandbox resumes a paused sandbox (beta feature).
func (c *Client) ResumeSandbox(ctx context.Context, sandboxID string, timeoutSeconds int) (*Sandbox, error) {
	// E2B Resume API requires a timeout in the request body
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300 // default 5 minutes
	}

	body, err := json.Marshal(&ResumeSandboxRequest{Timeout: timeoutSeconds})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sandboxes/"+sandboxID+"/resume", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	var result Sandbox
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

// setHeaders sets the common headers for E2B API requests.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
}

// checkResponse checks the HTTP response for errors.
func (c *Client) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)

	// Try to parse as API error
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Message != "" {
		apiErr.Code = resp.StatusCode
		return &apiErr
	}

	// Return generic error
	return &APIError{
		Code:    resp.StatusCode,
		Message: string(body),
	}
}

// IsNotFoundError checks if the error is a 404 not found error.
func IsNotFoundError(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == http.StatusNotFound
	}
	return false
}

// IsConflictError reports whether the API answered 409.
//
// E2B uses one status for two states that read as opposites: a sandbox that is
// paused when the caller wanted it running, and a sandbox that is running when
// the caller asked to resume it. Only the call site knows which, so the two
// named helpers below both delegate here rather than pretending to tell them
// apart.
func IsConflictError(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == http.StatusConflict
	}
	return false
}

// IsPausedError checks if the error indicates the sandbox is paused.
func IsPausedError(err error) bool {
	return IsConflictError(err)
}
