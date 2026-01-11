package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"go.uber.org/zap"
)

// Default timeout for tunnel creation.
const defaultTunnelCreateTimeout = 30 * time.Second

// TunnelManager manages tunnel lifecycle on the agent side.
// It handles tunnel creation requests and data relay.
type TunnelManager struct {
	logger       *zap.Logger
	relayManager *tunnel.RelayManager
	sender       MessageSender

	// Pending tunnel requests waiting for response
	pendingRequests   map[string]chan *pb.CreateTunnelResponse
	pendingRequestsMu sync.Mutex

	// Active tunnels
	tunnels   map[string]*TunnelInfo
	tunnelsMu sync.RWMutex

	// Persistent connections for WebSocket/bidirectional streams
	// Key: connectionID, Value: connection to local service
	persistentConns   map[string]*persistentConn
	persistentConnsMu sync.RWMutex

	// Current session ID (set when session is attached)
	sessionID   string
	sessionIDMu sync.RWMutex
}

// persistentConn represents a persistent connection to a local service.
type persistentConn struct {
	conn      net.Conn
	tunnelID  string
	localPort int
	cancel    context.CancelFunc
}

// Note: MessageSender is defined in task_runner.go

// TunnelInfo holds information about an active tunnel.
type TunnelInfo struct {
	ID        string
	Type      string
	LocalPort int
	Token     string
	PublicURL string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// TunnelManagerOption is a functional option for TunnelManager.
type TunnelManagerOption func(*TunnelManager)

// WithTMLogger sets the logger for the tunnel manager.
func WithTMLogger(logger *zap.Logger) TunnelManagerOption {
	return func(m *TunnelManager) {
		m.logger = logger
	}
}

// WithTMSender sets the message sender for the tunnel manager.
func WithTMSender(sender MessageSender) TunnelManagerOption {
	return func(m *TunnelManager) {
		m.sender = sender
	}
}

// NewTunnelManager creates a new TunnelManager.
func NewTunnelManager(opts ...TunnelManagerOption) *TunnelManager {
	m := &TunnelManager{
		logger:          zap.NewNop(),
		pendingRequests: make(map[string]chan *pb.CreateTunnelResponse),
		tunnels:         make(map[string]*TunnelInfo),
		persistentConns: make(map[string]*persistentConn),
	}

	for _, opt := range opts {
		opt(m)
	}

	// Create relay manager for handling tunnel data
	// The sendFrame callback sends data back to the server
	m.relayManager = tunnel.NewRelayManager(
		m.logger.Named("relay"),
		m.sendFrame,
	)

	return m
}

// sendFrame sends a tunnel frame to the server.
func (m *TunnelManager) sendFrame(frame *tunnel.Frame) error {
	if m.sender == nil {
		return errors.New("message sender not configured")
	}

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TunnelData{
			TunnelData: &pb.TunnelData{
				TunnelId: frame.TunnelID,
				Data:     frame.Payload,
				Eof:      frame.Type == tunnel.FrameTypeClose,
			},
		},
	}
	m.sender.Send(msg)
	return nil
}

// SetSessionID sets the current session ID.
func (m *TunnelManager) SetSessionID(sessionID string) {
	m.sessionIDMu.Lock()
	defer m.sessionIDMu.Unlock()
	m.sessionID = sessionID
}

// GetSessionID returns the current session ID.
func (m *TunnelManager) GetSessionID() string {
	m.sessionIDMu.RLock()
	defer m.sessionIDMu.RUnlock()
	return m.sessionID
}

// CreateTunnelParams contains parameters for creating a tunnel.
type CreateTunnelParams struct {
	Type      string // "http" or "tcp"
	LocalPort int    // Port on the agent to tunnel
	Timeout   time.Duration
}

// CreateTunnelResult contains the result of tunnel creation.
type CreateTunnelResult struct {
	TunnelID  string
	Token     string
	PublicURL string
	ExpiresAt time.Time
}

// CreateTunnel requests a new tunnel from the server.
// This is a blocking call that waits for the server's response.
func (m *TunnelManager) CreateTunnel(ctx context.Context, params *CreateTunnelParams) (*CreateTunnelResult, error) {
	sessionID := m.GetSessionID()
	if sessionID == "" {
		return nil, errors.New("no session attached")
	}

	if m.sender == nil {
		return nil, errors.New("message sender not configured")
	}

	// Validate params
	if params.LocalPort <= 0 || params.LocalPort > 65535 {
		return nil, errors.New("invalid local_port: must be between 1 and 65535")
	}

	tunnelType := params.Type
	if tunnelType == "" {
		tunnelType = "http"
	}

	timeout := params.Timeout
	if timeout <= 0 {
		timeout = defaultTunnelCreateTimeout
	}

	// Create a unique request ID
	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())

	// Create channel for response
	responseCh := make(chan *pb.CreateTunnelResponse, 1)
	m.pendingRequestsMu.Lock()
	m.pendingRequests[requestID] = responseCh
	m.pendingRequestsMu.Unlock()

	// Clean up on exit
	defer func() {
		m.pendingRequestsMu.Lock()
		delete(m.pendingRequests, requestID)
		m.pendingRequestsMu.Unlock()
	}()

	m.logger.Info("requesting tunnel creation",
		zap.String("session_id", sessionID),
		zap.String("type", tunnelType),
		zap.Int("local_port", params.LocalPort),
		zap.String("request_id", requestID),
	)

	// Send request to server
	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_CreateTunnelRequest{
			CreateTunnelRequest: &pb.CreateTunnelRequest{
				RequestId: requestID,
				SessionId: sessionID,
				Type:      tunnelType,
				LocalPort: int32(params.LocalPort),
				Direction: "outbound",
			},
		},
	}
	m.sender.Send(msg)

	// Wait for response with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("tunnel creation timed out: %w", ctx.Err())
	case resp := <-responseCh:
		if !resp.Success {
			return nil, fmt.Errorf("tunnel creation failed: %s", resp.Error)
		}

		// Store tunnel info
		info := &TunnelInfo{
			ID:        resp.TunnelId,
			Type:      tunnelType,
			LocalPort: params.LocalPort,
			Token:     resp.Token,
			PublicURL: resp.PublicUrl,
			ExpiresAt: time.UnixMilli(resp.ExpiresAtUnixMs),
			CreatedAt: time.Now(),
		}

		m.tunnelsMu.Lock()
		m.tunnels[resp.TunnelId] = info
		m.tunnelsMu.Unlock()

		m.logger.Info("tunnel created successfully",
			zap.String("tunnel_id", resp.TunnelId),
			zap.String("public_url", resp.PublicUrl),
			zap.Time("expires_at", info.ExpiresAt),
		)

		// For HTTP tunnels, we don't start a persistent relay connection.
		// Each HTTP request is handled independently in handleHTTPRequest.
		// For TCP tunnels, we need a persistent relay connection.
		if tunnelType != "http" {
			if err := m.startRelay(resp.TunnelId, params.LocalPort); err != nil {
				m.logger.Warn("failed to start relay",
					zap.String("tunnel_id", resp.TunnelId),
					zap.Error(err),
				)
				// Don't fail - the tunnel is created, relay can be retried
			}
		}

		return &CreateTunnelResult{
			TunnelID:  resp.TunnelId,
			Token:     resp.Token,
			PublicURL: resp.PublicUrl,
			ExpiresAt: info.ExpiresAt,
		}, nil
	}
}

// HandleCreateTunnelResponse handles a CreateTunnelResponse from the server.
func (m *TunnelManager) HandleCreateTunnelResponse(resp *pb.CreateTunnelResponse) {
	requestID := resp.GetRequestId()
	m.logger.Debug("received tunnel response",
		zap.String("request_id", requestID),
		zap.String("tunnel_id", resp.GetTunnelId()),
		zap.Bool("success", resp.GetSuccess()),
		zap.String("error", resp.GetError()),
	)

	m.pendingRequestsMu.Lock()
	defer m.pendingRequestsMu.Unlock()

	// Match response to pending request using request_id
	ch, ok := m.pendingRequests[requestID]
	if !ok {
		m.logger.Warn("no pending request for tunnel response",
			zap.String("request_id", requestID),
			zap.String("tunnel_id", resp.GetTunnelId()),
		)
		return
	}

	select {
	case ch <- resp:
	default:
		m.logger.Warn("response channel full, dropping response")
	}
}

// HandleTunnelData handles incoming tunnel data from the server.
func (m *TunnelManager) HandleTunnelData(ctx context.Context, data *pb.TunnelData) error {
	tunnelID := data.GetTunnelId()
	connectionID := data.GetConnectionId()

	m.logger.Debug("received tunnel data",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.Int("data_len", len(data.GetData())),
		zap.Bool("eof", data.GetEof()),
	)

	// Handle EOF - close persistent connection if exists
	if data.GetEof() {
		m.closePersistentConn(connectionID)
		return nil
	}

	// Check for existing persistent connection (WebSocket, etc.)
	m.persistentConnsMu.RLock()
	pc, hasPersistent := m.persistentConns[connectionID]
	m.persistentConnsMu.RUnlock()

	if hasPersistent {
		// Forward data to existing persistent connection
		if _, err := pc.conn.Write(data.GetData()); err != nil {
			m.logger.Error("failed to write to persistent connection",
				zap.String("connection_id", connectionID),
				zap.Error(err),
			)
			m.closePersistentConn(connectionID)
			return err
		}
		return nil
	}

	// Get tunnel info
	m.tunnelsMu.RLock()
	info, exists := m.tunnels[tunnelID]
	m.tunnelsMu.RUnlock()

	if !exists {
		return errors.New("tunnel not found")
	}

	// For HTTP tunnels, handle request-response pattern
	// Each connection_id represents a separate HTTP request
	if info.Type == "http" {
		go m.handleHTTPRequest(ctx, tunnelID, connectionID, info.LocalPort, data.GetData())
		return nil
	}

	// For TCP tunnels, use the relay manager (persistent connection)
	return m.relayManager.HandleFrame(&tunnel.Frame{
		Type:     tunnel.FrameTypeData,
		TunnelID: tunnelID,
		Payload:  data.GetData(),
	})
}

// handleHTTPRequest handles a single HTTP request-response cycle.
// Each HTTP request gets a fresh connection to the local service.
// Supports streaming responses (SSE, chunked) by forwarding data as it arrives.
// Supports WebSocket upgrade by establishing a persistent bidirectional connection.
func (m *TunnelManager) handleHTTPRequest(ctx context.Context, tunnelID, connectionID string, localPort int, requestData []byte) {
	localAddr := fmt.Sprintf("localhost:%d", localPort)

	// Check if this is a WebSocket upgrade request
	isWebSocket := m.isWebSocketUpgrade(requestData)

	m.logger.Debug("handling HTTP request",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.String("local_addr", localAddr),
		zap.Int("request_len", len(requestData)),
		zap.Bool("is_websocket", isWebSocket),
	)

	// Connect to local service
	conn, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
	if err != nil {
		m.logger.Error("failed to connect to local service",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.String("local_addr", localAddr),
			zap.Error(err),
		)
		m.sendHTTPErrorResponse(tunnelID, connectionID, err)
		return
	}

	// For WebSocket, handle separately and don't close the connection
	if isWebSocket {
		m.handleWebSocketUpgrade(ctx, tunnelID, connectionID, localPort, conn, requestData)
		return
	}

	defer func() { _ = conn.Close() }()

	// Forward request to local service
	if _, err := conn.Write(requestData); err != nil {
		m.logger.Error("failed to write request to local service",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		m.sendHTTPErrorResponse(tunnelID, connectionID, err)
		return
	}

	// Read response headers first
	// Set a short deadline for reading headers
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		m.logger.Error("failed to set read deadline", zap.Error(err))
		m.sendHTTPErrorResponse(tunnelID, connectionID, err)
		return
	}

	// Read until we have complete headers
	var headerBuf bytes.Buffer
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			m.logger.Error("failed to read response headers",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
				zap.Error(err),
			)
			m.sendHTTPErrorResponse(tunnelID, connectionID, err)
			return
		}
		headerBuf.Write(line)
		// Check for end of headers (empty line)
		if bytes.Equal(line, []byte("\r\n")) || bytes.Equal(line, []byte("\n")) {
			break
		}
	}

	// Parse the response to check if it's streaming
	headerBytes := headerBuf.Bytes()
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(headerBytes)), nil)
	if err != nil {
		m.logger.Error("failed to parse response headers",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		m.sendHTTPErrorResponse(tunnelID, connectionID, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Check if this is a streaming response
	contentType := resp.Header.Get("Content-Type")
	transferEncoding := resp.Header.Get("Transfer-Encoding")
	isStreaming := strings.HasPrefix(contentType, "text/event-stream") ||
		strings.EqualFold(transferEncoding, "chunked") ||
		resp.ContentLength < 0

	m.logger.Debug("response type detected",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.String("content_type", contentType),
		zap.String("transfer_encoding", transferEncoding),
		zap.Bool("is_streaming", isStreaming),
		zap.Int64("content_length", resp.ContentLength),
	)

	// Send headers immediately
	m.sendTunnelData(tunnelID, connectionID, headerBytes, false)

	if isStreaming {
		// For streaming responses, forward data as it arrives
		m.handleStreamingResponse(ctx, tunnelID, connectionID, conn, reader)
	} else {
		// For regular responses, read the complete body
		m.handleRegularResponse(tunnelID, connectionID, conn, reader, resp.ContentLength)
	}
}

// handleStreamingResponse forwards streaming response data (SSE, chunked) as it arrives.
func (m *TunnelManager) handleStreamingResponse(ctx context.Context, tunnelID, connectionID string, conn net.Conn, reader *bufio.Reader) {
	m.logger.Debug("starting streaming response relay",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
	)

	// For streaming, use a longer timeout or no timeout
	// Reset deadline to allow long-running streams
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		m.logger.Error("failed to clear read deadline", zap.Error(err))
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			m.logger.Debug("streaming context cancelled",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
			)
			m.sendTunnelData(tunnelID, connectionID, nil, true)
			return
		default:
		}

		// Set a short read deadline to allow checking context
		if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			m.logger.Error("failed to set read deadline", zap.Error(err))
			break
		}

		n, err := reader.Read(buf)
		if n > 0 {
			m.sendTunnelData(tunnelID, connectionID, buf[:n], false)
		}
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout is expected for streaming, continue
				continue
			}
			if err == io.EOF {
				m.logger.Debug("streaming response ended",
					zap.String("tunnel_id", tunnelID),
					zap.String("connection_id", connectionID),
				)
			} else {
				m.logger.Error("error reading streaming response",
					zap.String("tunnel_id", tunnelID),
					zap.String("connection_id", connectionID),
					zap.Error(err),
				)
			}
			break
		}
	}

	// Send EOF
	m.sendTunnelData(tunnelID, connectionID, nil, true)
}

// handleRegularResponse reads a complete HTTP response body.
func (m *TunnelManager) handleRegularResponse(tunnelID, connectionID string, conn net.Conn, reader *bufio.Reader, contentLength int64) {
	// Set deadline for reading body
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		m.logger.Error("failed to set read deadline", zap.Error(err))
	}

	var bodyData []byte
	if contentLength > 0 {
		// Known content length - read exactly that many bytes
		bodyData = make([]byte, contentLength)
		_, err := io.ReadFull(reader, bodyData)
		if err != nil {
			m.logger.Error("failed to read response body",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
				zap.Error(err),
			)
			// Send what we have
		}
	} else {
		// Unknown content length - read until EOF or timeout
		buf := make([]byte, 64*1024)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				bodyData = append(bodyData, buf[:n]...)
			}
			if err != nil {
				if err != io.EOF {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						break
					}
				}
				break
			}
		}
	}

	m.logger.Debug("sending HTTP response body",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.Int("body_len", len(bodyData)),
	)

	// Send body and EOF
	if len(bodyData) > 0 {
		m.sendTunnelData(tunnelID, connectionID, bodyData, false)
	}
	m.sendTunnelData(tunnelID, connectionID, nil, true)
}

// sendTunnelData sends data back to the server.
func (m *TunnelManager) sendTunnelData(tunnelID, connectionID string, data []byte, eof bool) {
	if m.sender == nil {
		return
	}
	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TunnelData{
			TunnelData: &pb.TunnelData{
				TunnelId:     tunnelID,
				ConnectionId: connectionID,
				Data:         data,
				Eof:          eof,
			},
		},
	}
	m.sender.Send(msg)
}

// sendHTTPErrorResponse sends an HTTP 502 error response back to the server.
func (m *TunnelManager) sendHTTPErrorResponse(tunnelID, connectionID string, err error) {
	errMsg := "Bad Gateway"
	if err != nil {
		errMsg = err.Error()
	}
	body := fmt.Sprintf("Error: %s", errMsg)
	response := fmt.Sprintf("HTTP/1.1 502 Bad Gateway\r\n"+
		"Content-Type: text/plain\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n"+
		"%s", len(body), body)

	if m.sender != nil {
		msg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_TunnelData{
				TunnelData: &pb.TunnelData{
					TunnelId:     tunnelID,
					ConnectionId: connectionID,
					Data:         []byte(response),
					Eof:          true,
				},
			},
		}
		m.sender.Send(msg)
	}
}

// startRelay starts the relay connection to the local port.
func (m *TunnelManager) startRelay(tunnelID string, localPort int) error {
	return m.relayManager.HandleConnect(tunnelID, localPort)
}

// HandleCreateTunnel handles a tunnel creation command from the server.
// This is used when the server initiates tunnel creation (e.g., via API).
func (m *TunnelManager) HandleCreateTunnel(tunnelID, tunnelType string, localPort int) error {
	m.logger.Info("handling create tunnel from server",
		zap.String("tunnel_id", tunnelID),
		zap.String("type", tunnelType),
		zap.Int("local_port", localPort),
	)

	// Store tunnel info (note: we don't have token/public_url for server-initiated tunnels)
	info := &TunnelInfo{
		ID:        tunnelID,
		Type:      tunnelType,
		LocalPort: localPort,
		CreatedAt: time.Now(),
	}

	m.tunnelsMu.Lock()
	m.tunnels[tunnelID] = info
	m.tunnelsMu.Unlock()

	// For HTTP tunnels, we don't start a persistent relay connection.
	// Each HTTP request is handled independently in handleHTTPRequest.
	// For TCP tunnels, we need a persistent relay connection.
	if tunnelType != "http" {
		if err := m.startRelay(tunnelID, localPort); err != nil {
			// Clean up on failure
			m.tunnelsMu.Lock()
			delete(m.tunnels, tunnelID)
			m.tunnelsMu.Unlock()
			return err
		}
		m.logger.Info("tunnel relay started",
			zap.String("tunnel_id", tunnelID),
			zap.Int("local_port", localPort),
		)
	} else {
		m.logger.Info("HTTP tunnel ready for requests",
			zap.String("tunnel_id", tunnelID),
			zap.Int("local_port", localPort),
		)
	}

	return nil
}

// GetTunnel returns information about an active tunnel.
func (m *TunnelManager) GetTunnel(tunnelID string) (*TunnelInfo, bool) {
	m.tunnelsMu.RLock()
	defer m.tunnelsMu.RUnlock()
	info, ok := m.tunnels[tunnelID]
	return info, ok
}

// ListTunnels returns all active tunnels.
func (m *TunnelManager) ListTunnels() []*TunnelInfo {
	m.tunnelsMu.RLock()
	defer m.tunnelsMu.RUnlock()

	result := make([]*TunnelInfo, 0, len(m.tunnels))
	for _, info := range m.tunnels {
		result = append(result, info)
	}
	return result
}

// CloseTunnel closes a tunnel.
func (m *TunnelManager) CloseTunnel(tunnelID string, reason string) error {
	m.tunnelsMu.Lock()
	_, exists := m.tunnels[tunnelID]
	if exists {
		delete(m.tunnels, tunnelID)
	}
	m.tunnelsMu.Unlock()

	if !exists {
		return errors.New("tunnel not found")
	}

	// Close relay
	if err := m.relayManager.HandleClose(tunnelID); err != nil {
		m.logger.Warn("failed to close relay",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
	}

	// Notify server
	if m.sender != nil {
		msg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_CloseTunnel{
				CloseTunnel: &pb.CloseTunnel{
					TunnelId: tunnelID,
					Reason:   reason,
				},
			},
		}
		m.sender.Send(msg)
	}

	m.logger.Info("tunnel closed",
		zap.String("tunnel_id", tunnelID),
		zap.String("reason", reason),
	)

	return nil
}

// isWebSocketUpgrade checks if the request data is a WebSocket upgrade request.
func (m *TunnelManager) isWebSocketUpgrade(requestData []byte) bool {
	// Check for WebSocket upgrade headers
	// Quick check for "Upgrade: websocket" header (case-insensitive)
	lower := strings.ToLower(string(requestData))
	return strings.Contains(lower, "upgrade: websocket") &&
		strings.Contains(lower, "connection:") &&
		strings.Contains(lower, "upgrade")
}

// handleWebSocketUpgrade handles a WebSocket upgrade request.
// It establishes a persistent bidirectional connection.
func (m *TunnelManager) handleWebSocketUpgrade(ctx context.Context, tunnelID, connectionID string, localPort int, conn net.Conn, requestData []byte) {
	m.logger.Info("handling WebSocket upgrade",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.Int("local_port", localPort),
	)

	// Forward the upgrade request to local service
	if _, err := conn.Write(requestData); err != nil {
		m.logger.Error("failed to write WebSocket upgrade request",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		m.sendHTTPErrorResponse(tunnelID, connectionID, err)
		_ = conn.Close()
		return
	}

	// Read the response from local service
	// Set a reasonable deadline for the upgrade response
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		m.logger.Error("failed to set read deadline", zap.Error(err))
		m.sendHTTPErrorResponse(tunnelID, connectionID, err)
		_ = conn.Close()
		return
	}

	// Read until we get the complete headers
	var headerBuf bytes.Buffer
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			m.logger.Error("failed to read WebSocket upgrade response",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
				zap.Error(err),
			)
			m.sendHTTPErrorResponse(tunnelID, connectionID, err)
			_ = conn.Close()
			return
		}
		headerBuf.Write(line)
		// Check for end of headers
		if bytes.Equal(line, []byte("\r\n")) || bytes.Equal(line, []byte("\n")) {
			break
		}
	}

	// Check if upgrade was successful (HTTP 101 Switching Protocols)
	headerBytes := headerBuf.Bytes()
	if !bytes.HasPrefix(headerBytes, []byte("HTTP/1.1 101")) {
		m.logger.Warn("WebSocket upgrade failed",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.String("response", string(headerBytes)),
		)
		// Send whatever response we got
		m.sendTunnelData(tunnelID, connectionID, headerBytes, true)
		_ = conn.Close()
		return
	}

	m.logger.Info("WebSocket upgrade successful",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
	)

	// Send the upgrade response to client
	m.sendTunnelData(tunnelID, connectionID, headerBytes, false)

	// Clear read deadline for ongoing bidirectional communication
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		m.logger.Error("failed to clear read deadline", zap.Error(err))
	}

	// Create a cancellable context for this connection
	connCtx, cancel := context.WithCancel(ctx)

	// Store the persistent connection
	pc := &persistentConn{
		conn:      conn,
		tunnelID:  tunnelID,
		localPort: localPort,
		cancel:    cancel,
	}

	m.persistentConnsMu.Lock()
	m.persistentConns[connectionID] = pc
	m.persistentConnsMu.Unlock()

	// Start a goroutine to read from local service and forward to server
	go m.relayFromLocalService(connCtx, tunnelID, connectionID, conn, reader)
}

// relayFromLocalService reads data from the local service and forwards to the server.
// This is used for persistent connections like WebSocket.
func (m *TunnelManager) relayFromLocalService(ctx context.Context, tunnelID, connectionID string, conn net.Conn, reader *bufio.Reader) {
	defer func() {
		m.closePersistentConn(connectionID)
		m.sendTunnelData(tunnelID, connectionID, nil, true)
	}()

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			m.logger.Debug("relay context cancelled",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
			)
			return
		default:
		}

		// Set a short read deadline to allow checking context
		if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			m.logger.Error("failed to set read deadline", zap.Error(err))
			return
		}

		n, err := reader.Read(buf)
		if n > 0 {
			m.sendTunnelData(tunnelID, connectionID, buf[:n], false)
		}
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout is expected, continue
				continue
			}
			if err == io.EOF {
				m.logger.Debug("local service connection closed",
					zap.String("tunnel_id", tunnelID),
					zap.String("connection_id", connectionID),
				)
			} else {
				m.logger.Error("error reading from local service",
					zap.String("tunnel_id", tunnelID),
					zap.String("connection_id", connectionID),
					zap.Error(err),
				)
			}
			return
		}
	}
}

// closePersistentConn closes and removes a persistent connection.
func (m *TunnelManager) closePersistentConn(connectionID string) {
	m.persistentConnsMu.Lock()
	pc, exists := m.persistentConns[connectionID]
	if exists {
		delete(m.persistentConns, connectionID)
	}
	m.persistentConnsMu.Unlock()

	if exists && pc != nil {
		m.logger.Debug("closing persistent connection",
			zap.String("connection_id", connectionID),
			zap.String("tunnel_id", pc.tunnelID),
		)
		pc.cancel()
		_ = pc.conn.Close()
	}
}
