package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// CDPConnection manages a WebSocket connection to Chrome DevTools Protocol.
type CDPConnection struct {
	mu sync.RWMutex

	endpoint string
	conn     *websocket.Conn

	// Message ID counter
	nextID atomic.Int64

	// Response channels keyed by message ID
	pending   map[int64]chan *cdpResponse
	pendingMu sync.Mutex

	// Event handlers
	eventHandlers   map[string]func(json.RawMessage)
	eventHandlersMu sync.RWMutex

	// Connection state
	closed   atomic.Bool
	closedCh chan struct{}
}

// cdpMessage represents a CDP request message.
type cdpMessage struct {
	ID        int64  `json:"id"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// cdpResponse represents a CDP response message.
type cdpResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
}

// cdpEvent represents a CDP event message.
type cdpEvent struct {
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId,omitempty"`
}

// cdpError represents a CDP error.
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *cdpError) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("CDP error %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("CDP error %d: %s", e.Code, e.Message)
}

// NewCDPConnection creates a new CDP connection.
func NewCDPConnection(endpoint string) *CDPConnection {
	return &CDPConnection{
		endpoint:      endpoint,
		pending:       make(map[int64]chan *cdpResponse),
		eventHandlers: make(map[string]func(json.RawMessage)),
		closedCh:      make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection.
func (c *CDPConnection) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return fmt.Errorf("already connected")
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, c.endpoint, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("dialing CDP endpoint: %w", err)
	}

	c.conn = conn

	// Start message receiver
	go c.receiveLoop()

	return nil
}

// Close closes the connection.
func (c *CDPConnection) Close() error {
	if c.closed.Swap(true) {
		return nil // Already closed
	}

	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	close(c.closedCh)

	if conn != nil {
		return conn.Close()
	}
	return nil
}

// IsClosed returns whether the connection is closed.
func (c *CDPConnection) IsClosed() bool {
	return c.closed.Load()
}

// OnEvent registers an event handler.
func (c *CDPConnection) OnEvent(method string, handler func(json.RawMessage)) {
	c.eventHandlersMu.Lock()
	defer c.eventHandlersMu.Unlock()
	c.eventHandlers[method] = handler
}

// Send sends a CDP command and waits for the response.
func (c *CDPConnection) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.SendWithSession(ctx, "", method, params)
}

// SendWithSession sends a CDP command with a session ID.
func (c *CDPConnection) SendWithSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("connection closed")
	}

	id := c.nextID.Add(1)
	msg := cdpMessage{
		ID:        id,
		Method:    method,
		Params:    params,
		SessionID: sessionID,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshaling message: %w", err)
	}

	// Create response channel
	respCh := make(chan *cdpResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	// Send message
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return nil, fmt.Errorf("sending message: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closedCh:
		return nil, fmt.Errorf("connection closed")
	}
}

// receiveLoop reads messages from the WebSocket.
func (c *CDPConnection) receiveLoop() {
	defer func() { _ = c.Close() }()

	for {
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if c.closed.Load() {
				return
			}
			// Connection error
			return
		}

		c.handleMessage(data)
	}
}

// handleMessage processes an incoming CDP message.
func (c *CDPConnection) handleMessage(data []byte) {
	// Try to parse as response first
	var resp cdpResponse
	if err := json.Unmarshal(data, &resp); err == nil && resp.ID > 0 {
		c.pendingMu.Lock()
		ch, ok := c.pending[resp.ID]
		c.pendingMu.Unlock()

		if ok {
			select {
			case ch <- &resp:
			default:
			}
		}
		return
	}

	// Parse as event
	var event cdpEvent
	if err := json.Unmarshal(data, &event); err == nil && event.Method != "" {
		c.eventHandlersMu.RLock()
		handler, ok := c.eventHandlers[event.Method]
		c.eventHandlersMu.RUnlock()

		if ok {
			handler(event.Params)
		}
	}
}

// =============================================================================
// CDP Command Helpers
// =============================================================================

// PageNavigateParams are parameters for Page.navigate.
type PageNavigateParams struct {
	URL      string `json:"url"`
	Referrer string `json:"referrer,omitempty"`
}

// PageNavigateResult is the result of Page.navigate.
type PageNavigateResult struct {
	FrameID   string `json:"frameId"`
	LoaderID  string `json:"loaderId,omitempty"`
	ErrorText string `json:"errorText,omitempty"`
}

// PageNavigate navigates to a URL.
func (c *CDPConnection) PageNavigate(ctx context.Context, url, referrer string) (*PageNavigateResult, error) {
	params := PageNavigateParams{URL: url, Referrer: referrer}
	result, err := c.Send(ctx, "Page.navigate", params)
	if err != nil {
		return nil, err
	}

	var nav PageNavigateResult
	if err := json.Unmarshal(result, &nav); err != nil {
		return nil, fmt.Errorf("parsing result: %w", err)
	}
	return &nav, nil
}

// ScreencastFrameParams are parameters for Page.startScreencast.
type ScreencastFrameParams struct {
	Format        string `json:"format,omitempty"`  // jpeg, png
	Quality       int    `json:"quality,omitempty"` // 0-100
	MaxWidth      int    `json:"maxWidth,omitempty"`
	MaxHeight     int    `json:"maxHeight,omitempty"`
	EveryNthFrame int    `json:"everyNthFrame,omitempty"`
}

// PageStartScreencast starts capturing screenshots.
func (c *CDPConnection) PageStartScreencast(ctx context.Context, params *ScreencastFrameParams) error {
	if params == nil {
		params = &ScreencastFrameParams{}
	}
	_, err := c.Send(ctx, "Page.startScreencast", params)
	return err
}

// PageStopScreencast stops capturing screenshots.
func (c *CDPConnection) PageStopScreencast(ctx context.Context) error {
	_, err := c.Send(ctx, "Page.stopScreencast", nil)
	return err
}

// PageScreencastFrameAck acknowledges a screencast frame.
func (c *CDPConnection) PageScreencastFrameAck(ctx context.Context, sessionID int) error {
	_, err := c.Send(ctx, "Page.screencastFrameAck", map[string]int{"sessionId": sessionID})
	return err
}

// ScreencastFrameEvent is the event data for Page.screencastFrame.
type ScreencastFrameEvent struct {
	Data      string                  `json:"data"` // Base64-encoded image data
	Metadata  ScreencastFrameMetadata `json:"metadata"`
	SessionID int                     `json:"sessionId"`
}

// ScreencastFrameMetadata contains frame metadata.
type ScreencastFrameMetadata struct {
	OffsetTop       float64 `json:"offsetTop"`
	PageScaleFactor float64 `json:"pageScaleFactor"`
	DeviceWidth     float64 `json:"deviceWidth"`
	DeviceHeight    float64 `json:"deviceHeight"`
	ScrollOffsetX   float64 `json:"scrollOffsetX"`
	ScrollOffsetY   float64 `json:"scrollOffsetY"`
	Timestamp       float64 `json:"timestamp,omitempty"`
}

// PageCaptureScreenshotParams are parameters for Page.captureScreenshot.
type PageCaptureScreenshotParams struct {
	Format                string    `json:"format,omitempty"`  // jpeg, png, webp
	Quality               int       `json:"quality,omitempty"` // 0-100 for jpeg/webp
	Clip                  *Viewport `json:"clip,omitempty"`
	FromSurface           bool      `json:"fromSurface,omitempty"`
	CaptureBeyondViewport bool      `json:"captureBeyondViewport,omitempty"`
	OptimizeForSpeed      bool      `json:"optimizeForSpeed,omitempty"`
}

// Viewport represents a page viewport.
type Viewport struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Scale  float64 `json:"scale"`
}

// PageCaptureScreenshot captures a screenshot.
func (c *CDPConnection) PageCaptureScreenshot(ctx context.Context, params *PageCaptureScreenshotParams) ([]byte, error) {
	if params == nil {
		params = &PageCaptureScreenshotParams{}
	}

	result, err := c.Send(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parsing result: %w", err)
	}

	return base64.StdEncoding.DecodeString(resp.Data)
}

// InputDispatchMouseEventParams are parameters for Input.dispatchMouseEvent.
type InputDispatchMouseEventParams struct {
	Type        string  `json:"type"` // mousePressed, mouseReleased, mouseMoved, mouseWheel
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Modifiers   int     `json:"modifiers,omitempty"` // Bitfield: 1=Alt, 2=Ctrl, 4=Meta, 8=Shift
	Timestamp   float64 `json:"timestamp,omitempty"` // Seconds since epoch
	Button      string  `json:"button,omitempty"`    // none, left, middle, right, back, forward
	Buttons     int     `json:"buttons,omitempty"`   // Buttons pressed
	ClickCount  int     `json:"clickCount,omitempty"`
	DeltaX      float64 `json:"deltaX,omitempty"`      // For mouseWheel
	DeltaY      float64 `json:"deltaY,omitempty"`      // For mouseWheel
	PointerType string  `json:"pointerType,omitempty"` // mouse, pen
}

// InputDispatchMouseEvent dispatches a mouse event.
func (c *CDPConnection) InputDispatchMouseEvent(ctx context.Context, params *InputDispatchMouseEventParams) error {
	_, err := c.Send(ctx, "Input.dispatchMouseEvent", params)
	return err
}

// InputDispatchKeyEventParams are parameters for Input.dispatchKeyEvent.
type InputDispatchKeyEventParams struct {
	Type                  string  `json:"type"` // keyDown, keyUp, rawKeyDown, char
	Modifiers             int     `json:"modifiers,omitempty"`
	Timestamp             float64 `json:"timestamp,omitempty"`
	Text                  string  `json:"text,omitempty"`
	UnmodifiedText        string  `json:"unmodifiedText,omitempty"`
	KeyIdentifier         string  `json:"keyIdentifier,omitempty"`
	Code                  string  `json:"code,omitempty"`
	Key                   string  `json:"key,omitempty"`
	WindowsVirtualKeyCode int     `json:"windowsVirtualKeyCode,omitempty"`
	NativeVirtualKeyCode  int     `json:"nativeVirtualKeyCode,omitempty"`
	AutoRepeat            bool    `json:"autoRepeat,omitempty"`
	IsKeypad              bool    `json:"isKeypad,omitempty"`
	IsSystemKey           bool    `json:"isSystemKey,omitempty"`
	Location              int     `json:"location,omitempty"` // 0=Standard, 1=Left, 2=Right, 3=Numpad
}

// InputDispatchKeyEvent dispatches a keyboard event.
func (c *CDPConnection) InputDispatchKeyEvent(ctx context.Context, params *InputDispatchKeyEventParams) error {
	_, err := c.Send(ctx, "Input.dispatchKeyEvent", params)
	return err
}

// TargetInfo contains information about a target.
type TargetInfo struct {
	TargetID         string `json:"targetId"`
	Type             string `json:"type"` // page, background_page, service_worker, etc.
	Title            string `json:"title"`
	URL              string `json:"url"`
	Attached         bool   `json:"attached"`
	OpenerID         string `json:"openerId,omitempty"`
	CanAccessOpener  bool   `json:"canAccessOpener"`
	BrowserContextID string `json:"browserContextId,omitempty"`
}

// TargetGetTargets returns information about available targets.
func (c *CDPConnection) TargetGetTargets(ctx context.Context) ([]*TargetInfo, error) {
	result, err := c.Send(ctx, "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		TargetInfos []*TargetInfo `json:"targetInfos"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parsing result: %w", err)
	}
	return resp.TargetInfos, nil
}

// TargetActivateTarget activates (focuses) the specified target.
func (c *CDPConnection) TargetActivateTarget(ctx context.Context, targetID string) error {
	_, err := c.Send(ctx, "Target.activateTarget", map[string]string{"targetId": targetID})
	return err
}

// BrowserGetVersion returns browser version information.
func (c *CDPConnection) BrowserGetVersion(ctx context.Context) (*BrowserInfo, error) {
	result, err := c.Send(ctx, "Browser.getVersion", nil)
	if err != nil {
		return nil, err
	}

	var info BrowserInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("parsing result: %w", err)
	}
	return &info, nil
}

// PageEnable enables page domain events.
func (c *CDPConnection) PageEnable(ctx context.Context) error {
	_, err := c.Send(ctx, "Page.enable", nil)
	return err
}

// RuntimeEnable enables runtime domain events.
func (c *CDPConnection) RuntimeEnable(ctx context.Context) error {
	_, err := c.Send(ctx, "Runtime.enable", nil)
	return err
}

// TargetSetDiscoverTargets enables/disables target discovery.
func (c *CDPConnection) TargetSetDiscoverTargets(ctx context.Context, discover bool) error {
	_, err := c.Send(ctx, "Target.setDiscoverTargets", map[string]bool{"discover": discover})
	return err
}

// EmulationSetDeviceMetricsOverrideParams are parameters for Emulation.setDeviceMetricsOverride.
type EmulationSetDeviceMetricsOverrideParams struct {
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	DeviceScaleFactor float64 `json:"deviceScaleFactor"`
	Mobile            bool    `json:"mobile"`
}

// EmulationSetDeviceMetricsOverride sets device metrics override.
func (c *CDPConnection) EmulationSetDeviceMetricsOverride(ctx context.Context, width, height int, scaleFactor float64, mobile bool) error {
	params := EmulationSetDeviceMetricsOverrideParams{
		Width:             width,
		Height:            height,
		DeviceScaleFactor: scaleFactor,
		Mobile:            mobile,
	}
	_, err := c.Send(ctx, "Emulation.setDeviceMetricsOverride", params)
	return err
}
