package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Frame types for the tunnel protocol.
const (
	FrameTypeData    = 0x01 // Data frame
	FrameTypeClose   = 0x02 // Close frame
	FrameTypePing    = 0x03 // Ping frame (keepalive)
	FrameTypePong    = 0x04 // Pong frame (keepalive response)
	FrameTypeConnect = 0x05 // Connection established
	FrameTypeError   = 0x06 // Error frame
)

// Frame header size: type (1 byte) + length (4 bytes) + tunnel ID (16 bytes)
const frameHeaderSize = 21

// MaxFrameSize is the maximum size of a single frame payload.
const MaxFrameSize = 64 * 1024 // 64KB

// Frame represents a tunnel protocol frame.
type Frame struct {
	Type     byte
	TunnelID string
	Payload  []byte
}

// MarshalBinary encodes a frame for transmission.
func (f *Frame) MarshalBinary() ([]byte, error) {
	tunnelIDBytes := []byte(f.TunnelID)
	if len(tunnelIDBytes) > 255 {
		return nil, errors.New("tunnel ID too long")
	}

	// Format: type (1) + tunnel_id_len (1) + tunnel_id + payload_len (4) + payload
	size := 1 + 1 + len(tunnelIDBytes) + 4 + len(f.Payload)
	buf := make([]byte, size)

	buf[0] = f.Type
	buf[1] = byte(len(tunnelIDBytes))
	copy(buf[2:2+len(tunnelIDBytes)], tunnelIDBytes)
	binary.BigEndian.PutUint32(buf[2+len(tunnelIDBytes):], uint32(len(f.Payload)))
	copy(buf[6+len(tunnelIDBytes):], f.Payload)

	return buf, nil
}

// UnmarshalBinary decodes a frame from binary data.
func (f *Frame) UnmarshalBinary(data []byte) error {
	if len(data) < 6 { // minimum: type + id_len + payload_len
		return errors.New("frame too short")
	}

	f.Type = data[0]
	idLen := int(data[1])
	if len(data) < 2+idLen+4 {
		return errors.New("frame too short for tunnel ID")
	}

	f.TunnelID = string(data[2 : 2+idLen])
	payloadLen := binary.BigEndian.Uint32(data[2+idLen:])
	if len(data) < 6+idLen+int(payloadLen) {
		return errors.New("frame too short for payload")
	}

	f.Payload = make([]byte, payloadLen)
	copy(f.Payload, data[6+idLen:])

	return nil
}

// RelayConnection manages a single tunnel relay connection.
type RelayConnection struct {
	tunnelID  string
	localAddr string // e.g., "localhost:3000"
	localConn net.Conn
	logger    *zap.Logger

	// Channel for sending data to the remote side
	sendCh chan []byte

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// State
	connected bool
	mu        sync.RWMutex
}

// NewRelayConnection creates a new relay connection for a tunnel.
func NewRelayConnection(tunnelID, localAddr string, logger *zap.Logger) *RelayConnection {
	ctx, cancel := context.WithCancel(context.Background())
	return &RelayConnection{
		tunnelID:  tunnelID,
		localAddr: localAddr,
		logger:    logger.With(zap.String("tunnel_id", tunnelID)),
		sendCh:    make(chan []byte, 256),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Connect establishes a connection to the local service.
func (r *RelayConnection) Connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.connected {
		return nil
	}

	conn, err := net.DialTimeout("tcp", r.localAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to local service %s: %w", r.localAddr, err)
	}

	r.localConn = conn
	r.connected = true

	r.logger.Info("connected to local service",
		zap.String("local_addr", r.localAddr),
	)

	return nil
}

// IsConnected returns true if connected to the local service.
func (r *RelayConnection) IsConnected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.connected
}

// SendToLocal writes data to the local service.
func (r *RelayConnection) SendToLocal(data []byte) error {
	r.mu.RLock()
	conn := r.localConn
	r.mu.RUnlock()

	if conn == nil {
		return ErrConnectionFailed
	}

	// Set write deadline
	if err := conn.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}

	_, err := conn.Write(data)
	return err
}

// ReadFromLocal reads data from the local service.
// This should be called in a goroutine to continuously read data.
func (r *RelayConnection) ReadFromLocal(onData func([]byte) error) error {
	r.mu.RLock()
	conn := r.localConn
	r.mu.RUnlock()

	if conn == nil {
		return ErrConnectionFailed
	}

	buf := make([]byte, MaxFrameSize)
	for {
		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		default:
		}

		// Set read deadline
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			return err
		}

		n, err := conn.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				r.logger.Debug("local connection closed")
				return nil
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout is ok, continue reading
				continue
			}
			return err
		}

		if n > 0 {
			// Make a copy of the data
			data := make([]byte, n)
			copy(data, buf[:n])

			if err := onData(data); err != nil {
				return err
			}
		}
	}
}

// Close closes the relay connection.
func (r *RelayConnection) Close() error {
	r.cancel()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.localConn != nil {
		if err := r.localConn.Close(); err != nil {
			r.logger.Warn("error closing local connection", zap.Error(err))
		}
		r.localConn = nil
	}
	r.connected = false

	r.logger.Debug("relay connection closed")
	return nil
}

// RelayManager manages multiple relay connections on the agent side.
type RelayManager struct {
	connections map[string]*RelayConnection
	mu          sync.RWMutex
	logger      *zap.Logger

	// Callback for sending frames back to the server
	sendFrame func(*Frame) error
}

// NewRelayManager creates a new relay manager.
func NewRelayManager(logger *zap.Logger, sendFrame func(*Frame) error) *RelayManager {
	return &RelayManager{
		connections: make(map[string]*RelayConnection),
		logger:      logger.Named("relay"),
		sendFrame:   sendFrame,
	}
}

// HandleConnect handles a connect request for a new tunnel.
func (m *RelayManager) HandleConnect(tunnelID string, localPort int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.connections[tunnelID]; exists {
		return errors.New("relay already exists for tunnel")
	}

	// Create relay connection
	localAddr := fmt.Sprintf("localhost:%d", localPort)
	relay := NewRelayConnection(tunnelID, localAddr, m.logger)

	// Connect to local service
	if err := relay.Connect(); err != nil {
		return err
	}

	// Store the connection
	m.connections[tunnelID] = relay

	// Start reading from local service
	go m.readLoop(relay)

	// Send connect acknowledgment
	if m.sendFrame != nil {
		if err := m.sendFrame(&Frame{
			Type:     FrameTypeConnect,
			TunnelID: tunnelID,
		}); err != nil {
			m.logger.Warn("failed to send connect frame", zap.Error(err))
		}
	}

	return nil
}

// readLoop continuously reads from the local connection and sends frames.
func (m *RelayManager) readLoop(relay *RelayConnection) {
	err := relay.ReadFromLocal(func(data []byte) error {
		if m.sendFrame != nil {
			return m.sendFrame(&Frame{
				Type:     FrameTypeData,
				TunnelID: relay.tunnelID,
				Payload:  data,
			})
		}
		return nil
	})

	if err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Warn("relay read loop error",
			zap.String("tunnel_id", relay.tunnelID),
			zap.Error(err),
		)
	}

	// Connection closed, send close frame
	if m.sendFrame != nil {
		_ = m.sendFrame(&Frame{
			Type:     FrameTypeClose,
			TunnelID: relay.tunnelID,
		})
	}
}

// HandleData handles incoming data for a tunnel.
func (m *RelayManager) HandleData(tunnelID string, data []byte) error {
	m.mu.RLock()
	relay, exists := m.connections[tunnelID]
	m.mu.RUnlock()

	if !exists {
		return ErrTunnelNotFound
	}

	return relay.SendToLocal(data)
}

// HandleClose handles a close request for a tunnel.
func (m *RelayManager) HandleClose(tunnelID string) error {
	m.mu.Lock()
	relay, exists := m.connections[tunnelID]
	if exists {
		delete(m.connections, tunnelID)
	}
	m.mu.Unlock()

	if !exists {
		return ErrTunnelNotFound
	}

	return relay.Close()
}

// CloseAll closes all relay connections.
func (m *RelayManager) CloseAll() {
	m.mu.Lock()
	connections := m.connections
	m.connections = make(map[string]*RelayConnection)
	m.mu.Unlock()

	for _, relay := range connections {
		_ = relay.Close()
	}
}

// GetActiveCount returns the number of active relay connections.
func (m *RelayManager) GetActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.connections)
}

// HandleFrame dispatches an incoming frame to the appropriate handler.
func (m *RelayManager) HandleFrame(frame *Frame) error {
	switch frame.Type {
	case FrameTypeData:
		return m.HandleData(frame.TunnelID, frame.Payload)
	case FrameTypeClose:
		return m.HandleClose(frame.TunnelID)
	case FrameTypePing:
		// Respond with pong
		if m.sendFrame != nil {
			return m.sendFrame(&Frame{
				Type:     FrameTypePong,
				TunnelID: frame.TunnelID,
			})
		}
		return nil
	default:
		m.logger.Warn("unknown frame type", zap.Int("type", int(frame.Type)))
		return nil
	}
}
