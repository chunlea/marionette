package tunnel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockConnection implements Connection for testing.
type mockConnection struct {
	readBuf    *bytes.Buffer
	writeBuf   *bytes.Buffer
	readErr    error
	writeErr   error
	closed     bool
	mu         sync.Mutex
	readBlock  chan struct{} // Channel to block reads until signaled
	writeBlock chan struct{} // Channel to block writes until signaled
}

func newMockConnection() *mockConnection {
	return &mockConnection{
		readBuf:    bytes.NewBuffer(nil),
		writeBuf:   bytes.NewBuffer(nil),
		readBlock:  make(chan struct{}),
		writeBlock: make(chan struct{}),
	}
}

func (m *mockConnection) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return 0, io.EOF
	}
	if m.readErr != nil {
		err := m.readErr
		m.mu.Unlock()
		return 0, err
	}
	if m.readBuf.Len() == 0 {
		// Block until data is available or closed
		m.mu.Unlock()
		select {
		case <-m.readBlock:
			m.mu.Lock()
			if m.closed {
				m.mu.Unlock()
				return 0, io.EOF
			}
			n, err = m.readBuf.Read(p)
			m.mu.Unlock()
			return n, err
		}
	}
	n, err = m.readBuf.Read(p)
	m.mu.Unlock()
	return n, err
}

func (m *mockConnection) Write(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return m.writeBuf.Write(p)
}

func (m *mockConnection) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	close(m.readBlock)
	return nil
}

func (m *mockConnection) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockConnection) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockConnection) SetWriteDeadline(t time.Time) error {
	return nil
}

func (m *mockConnection) WriteToReadBuf(data []byte) {
	m.mu.Lock()
	m.readBuf.Write(data)
	m.mu.Unlock()
	// Signal that data is available
	select {
	case m.readBlock <- struct{}{}:
	default:
	}
}

func (m *mockConnection) GetWrittenData() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuf.Bytes()
}

// mockTCPConnectionHandler implements ConnectionHandler for TCP testing.
type mockTCPConnectionHandler struct {
	connected     bool
	sendData      [][]byte
	receiveData   [][]byte
	receiveIdx    int
	sendErr       error
	receiveErr    error
	mu            sync.Mutex
	receiveWaitCh chan struct{}
	sendNotifyCh  chan []byte
}

func newMockTCPConnectionHandler() *mockTCPConnectionHandler {
	return &mockTCPConnectionHandler{
		connected:     true,
		receiveWaitCh: make(chan struct{}),
		sendNotifyCh:  make(chan []byte, 100),
	}
}

func (m *mockTCPConnectionHandler) SendTunnelData(ctx context.Context, tunnelID string, data []byte) error {
	m.mu.Lock()
	if m.sendErr != nil {
		err := m.sendErr
		m.mu.Unlock()
		return err
	}
	// Make a copy of the data
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.sendData = append(m.sendData, dataCopy)
	m.mu.Unlock()

	// Notify that data was sent
	select {
	case m.sendNotifyCh <- dataCopy:
	default:
	}

	return nil
}

func (m *mockTCPConnectionHandler) ReceiveTunnelData(ctx context.Context, tunnelID string) ([]byte, error) {
	m.mu.Lock()
	if m.receiveErr != nil {
		err := m.receiveErr
		m.mu.Unlock()
		return nil, err
	}
	if m.receiveIdx >= len(m.receiveData) {
		m.mu.Unlock()
		// Block until more data is available or context cancelled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-m.receiveWaitCh:
			m.mu.Lock()
			if m.receiveIdx >= len(m.receiveData) {
				m.mu.Unlock()
				return nil, ctx.Err()
			}
			data := m.receiveData[m.receiveIdx]
			m.receiveIdx++
			m.mu.Unlock()
			return data, nil
		}
	}
	data := m.receiveData[m.receiveIdx]
	m.receiveIdx++
	m.mu.Unlock()
	return data, nil
}

func (m *mockTCPConnectionHandler) CloseTunnel(ctx context.Context, tunnelID string) error {
	return nil
}

func (m *mockTCPConnectionHandler) IsConnected() bool {
	return m.connected
}

func (m *mockTCPConnectionHandler) AddReceiveData(data []byte) {
	m.mu.Lock()
	m.receiveData = append(m.receiveData, data)
	m.mu.Unlock()
	// Signal that data is available
	select {
	case m.receiveWaitCh <- struct{}{}:
	default:
	}
}

func (m *mockTCPConnectionHandler) GetSentData() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendData
}

func TestTCPProxy_ProxyTCPConnection_BidirectionalRelay(t *testing.T) {
	config := DefaultTCPProxyConfig()
	config.IdleTimeout = 100 * time.Millisecond
	proxy := NewTCPProxy(config)

	handler := newMockTCPConnectionHandler()
	conn := newMockConnection()

	// Set up test data
	clientData := []byte("hello from client")
	runnerData := []byte("hello from runner")

	// Start proxy in a goroutine
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- proxy.ProxyTCPConnection(ctx, "tun_test", handler, conn)
	}()

	// Simulate client sending data
	conn.WriteToReadBuf(clientData)

	// Wait for data to be sent to handler
	select {
	case sent := <-handler.sendNotifyCh:
		assert.Equal(t, clientData, sent)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for client data to be sent")
	}

	// Simulate runner sending data back
	handler.AddReceiveData(runnerData)

	// Give some time for data to be written to connection
	time.Sleep(100 * time.Millisecond)

	// Close connection to end the proxy
	_ = conn.Close()

	// Wait for proxy to complete
	select {
	case err := <-errCh:
		// Error might be context cancelled or EOF, both are acceptable
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			// Check for expected errors during shutdown
			assert.Contains(t, err.Error(), "client to runner relay failed")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for proxy to complete")
	}

	// Verify client data was sent to runner
	sentData := handler.GetSentData()
	require.Len(t, sentData, 1)
	assert.Equal(t, clientData, sentData[0])

	// Verify runner data was written to client
	writtenData := conn.GetWrittenData()
	assert.Equal(t, runnerData, writtenData)
}

func TestTCPProxy_ProxyTCPConnection_SendError(t *testing.T) {
	proxy := NewTCPProxy(DefaultTCPProxyConfig())

	handler := newMockTCPConnectionHandler()
	handler.sendErr = assert.AnError
	conn := newMockConnection()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Write data to trigger send
	conn.WriteToReadBuf([]byte("test"))

	err := proxy.ProxyTCPConnection(ctx, "tun_test", handler, conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send to runner")
}

func TestTCPProxy_ProxyTCPConnection_ReceiveError(t *testing.T) {
	proxy := NewTCPProxy(DefaultTCPProxyConfig())

	handler := newMockTCPConnectionHandler()
	handler.receiveErr = assert.AnError
	conn := newMockConnection()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := proxy.ProxyTCPConnection(ctx, "tun_test", handler, conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to receive from runner")
}

func TestTCPProxy_ProxyTCPConnection_ContextCancellation(t *testing.T) {
	proxy := NewTCPProxy(DefaultTCPProxyConfig())

	handler := newMockTCPConnectionHandler()
	conn := newMockConnection()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- proxy.ProxyTCPConnection(ctx, "tun_test", handler, conn)
	}()

	// Cancel context
	cancel()

	select {
	case err := <-errCh:
		// Should complete without hanging
		if err != nil {
			// Error should be related to context cancellation
			assert.True(t, errors.Is(err, context.Canceled) ||
				strings.Contains(err.Error(), "context canceled"),
				"unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for proxy to complete after cancellation")
	}
}

func TestDefaultTCPProxyConfig(t *testing.T) {
	config := DefaultTCPProxyConfig()

	assert.Greater(t, config.BufferSize, 0)
	assert.Greater(t, config.IdleTimeout, time.Duration(0))
	assert.Greater(t, config.MaxConnectionDuration, time.Duration(0))
}

func TestNewTCPProxy(t *testing.T) {
	config := TCPProxyConfig{
		BufferSize:            64 * 1024,
		IdleTimeout:           10 * time.Minute,
		MaxConnectionDuration: 2 * time.Hour,
	}

	proxy := NewTCPProxy(config)

	assert.Equal(t, config.BufferSize, proxy.config.BufferSize)
	assert.Equal(t, config.IdleTimeout, proxy.config.IdleTimeout)
	assert.Equal(t, config.MaxConnectionDuration, proxy.config.MaxConnectionDuration)
}

func TestTunnelManager_HandleTCPConnection_TypeMismatch(t *testing.T) {
	// Create a tunnel manager with an HTTP tunnel (not TCP)
	manager := NewTunnelManager(
		WithIDGen(func() string { return "tun_test" }),
	)

	// Register a handler
	handler := &mockHTTPConnectionHandler{connected: true}
	manager.RegisterHandler("run_456", handler)

	// Create an HTTP tunnel
	tunnel, err := manager.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      TypeHTTP, // HTTP, not TCP
		LocalPort: 3000,
	})
	require.NoError(t, err)

	// Try to handle as TCP connection
	conn := newMockConnection()
	err = manager.HandleTCPConnection(context.Background(), tunnel.ID, conn)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel type mismatch")
}
