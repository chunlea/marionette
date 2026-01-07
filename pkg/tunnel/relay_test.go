package tunnel

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestFrame_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
	}{
		{
			name: "data frame",
			frame: Frame{
				Type:     FrameTypeData,
				TunnelID: "tun_test123",
				Payload:  []byte("hello world"),
			},
		},
		{
			name: "close frame",
			frame: Frame{
				Type:     FrameTypeClose,
				TunnelID: "tun_test456",
				Payload:  nil,
			},
		},
		{
			name: "ping frame",
			frame: Frame{
				Type:     FrameTypePing,
				TunnelID: "tun_abc",
				Payload:  []byte{},
			},
		},
		{
			name: "large payload",
			frame: Frame{
				Type:     FrameTypeData,
				TunnelID: "tun_large",
				Payload:  make([]byte, 10000),
			},
		},
		{
			name: "empty payload",
			frame: Frame{
				Type:     FrameTypeConnect,
				TunnelID: "tun_empty",
				Payload:  nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := tt.frame.MarshalBinary()
			require.NoError(t, err)
			require.NotEmpty(t, data)

			// Unmarshal
			var decoded Frame
			err = decoded.UnmarshalBinary(data)
			require.NoError(t, err)

			// Verify
			assert.Equal(t, tt.frame.Type, decoded.Type)
			assert.Equal(t, tt.frame.TunnelID, decoded.TunnelID)
			if tt.frame.Payload == nil {
				assert.Empty(t, decoded.Payload)
			} else {
				assert.Equal(t, tt.frame.Payload, decoded.Payload)
			}
		})
	}
}

func TestFrame_MarshalError(t *testing.T) {
	// Test tunnel ID that's too long
	longID := make([]byte, 300)
	for i := range longID {
		longID[i] = 'a'
	}

	frame := Frame{
		Type:     FrameTypeData,
		TunnelID: string(longID),
		Payload:  []byte("test"),
	}

	_, err := frame.MarshalBinary()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel ID too long")
}

func TestFrame_UnmarshalError(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "too short",
			data: []byte{0x01, 0x02},
		},
		{
			name: "truncated tunnel ID",
			data: []byte{0x01, 0x10, 0x01, 0x02, 0x03}, // claims 16 bytes but only 3
		},
		{
			name: "truncated payload",
			data: []byte{0x01, 0x03, 'a', 'b', 'c', 0x00, 0x00, 0x00, 0x10}, // claims 16 bytes payload but none
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var frame Frame
			err := frame.UnmarshalBinary(tt.data)
			assert.Error(t, err)
		})
	}
}

func TestFrameTypeConstants(t *testing.T) {
	// Verify frame type values are distinct
	types := []byte{
		FrameTypeData,
		FrameTypeClose,
		FrameTypePing,
		FrameTypePong,
		FrameTypeConnect,
		FrameTypeError,
	}

	seen := make(map[byte]bool)
	for _, typ := range types {
		assert.False(t, seen[typ], "duplicate frame type: %d", typ)
		seen[typ] = true
	}
}

func TestRelayConnection_New(t *testing.T) {
	logger := zaptest.NewLogger(t)
	relay := NewRelayConnection("tun_123", "localhost:3000", logger)

	assert.Equal(t, "tun_123", relay.tunnelID)
	assert.Equal(t, "localhost:3000", relay.localAddr)
	assert.NotNil(t, relay.sendCh)
	assert.NotNil(t, relay.ctx)
	assert.NotNil(t, relay.cancel)
	assert.False(t, relay.IsConnected())
}

func TestRelayConnection_ConnectAndClose(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	relay := NewRelayConnection("tun_123", listener.Addr().String(), logger)

	// Test connect
	err = relay.Connect()
	require.NoError(t, err)
	assert.True(t, relay.IsConnected())

	// Test double connect (should be no-op)
	err = relay.Connect()
	require.NoError(t, err)

	// Test close
	err = relay.Close()
	require.NoError(t, err)
	assert.False(t, relay.IsConnected())
}

func TestRelayConnection_ConnectError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Use an address that should fail
	relay := NewRelayConnection("tun_123", "127.0.0.1:1", logger)

	err := relay.Connect()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")
	assert.False(t, relay.IsConnected())
}

func TestRelayConnection_SendToLocal(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start a test server that reads data
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	received := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		if n > 0 {
			received <- buf[:n]
		}
	}()

	relay := NewRelayConnection("tun_123", listener.Addr().String(), logger)
	err = relay.Connect()
	require.NoError(t, err)
	defer func() { _ = relay.Close() }()

	// Send data
	testData := []byte("hello from relay")
	err = relay.SendToLocal(testData)
	require.NoError(t, err)

	// Verify received
	select {
	case data := <-received:
		assert.Equal(t, testData, data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for data")
	}
}

func TestRelayConnection_SendToLocalNotConnected(t *testing.T) {
	logger := zaptest.NewLogger(t)
	relay := NewRelayConnection("tun_123", "localhost:3000", logger)

	err := relay.SendToLocal([]byte("test"))
	assert.Error(t, err)
	assert.Equal(t, ErrConnectionFailed, err)
}

func TestRelayManager_New(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sendFn := func(f *Frame) error { return nil }

	manager := NewRelayManager(logger, sendFn)
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.connections)
	assert.NotNil(t, manager.sendFrame)
	assert.Equal(t, 0, manager.GetActiveCount())
}

func TestRelayManager_HandleConnect(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	// Extract port
	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	var port int
	// Get port as int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Keep connection open
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 1024)
				for {
					_, err := c.Read(buf)
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	var frames []Frame
	var mu sync.Mutex
	sendFn := func(f *Frame) error {
		mu.Lock()
		frames = append(frames, *f)
		mu.Unlock()
		return nil
	}

	manager := NewRelayManager(logger, sendFn)

	t.Run("connect to local service", func(t *testing.T) {
		err := manager.HandleConnect("tun_123", port)
		require.NoError(t, err)
		assert.Equal(t, 1, manager.GetActiveCount())

		// Wait for connect frame
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		hasConnectFrame := false
		for _, f := range frames {
			if f.Type == FrameTypeConnect && f.TunnelID == "tun_123" {
				hasConnectFrame = true
				break
			}
		}
		mu.Unlock()
		assert.True(t, hasConnectFrame, "should have sent connect frame")
	})

	t.Run("duplicate connect", func(t *testing.T) {
		err := manager.HandleConnect("tun_123", port)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestRelayManager_HandleData(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start a test server that echoes data
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	received := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		if n > 0 {
			received <- buf[:n]
		}
	}()

	// Extract port
	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	manager := NewRelayManager(logger, nil)
	err = manager.HandleConnect("tun_data", port)
	require.NoError(t, err)

	// Send data through relay
	testData := []byte("relay test data")
	err = manager.HandleData("tun_data", testData)
	require.NoError(t, err)

	// Verify received
	select {
	case data := <-received:
		assert.Equal(t, testData, data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for data")
	}
}

func TestRelayManager_HandleDataNotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewRelayManager(logger, nil)

	err := manager.HandleData("non_existent", []byte("test"))
	assert.Error(t, err)
	assert.Equal(t, ErrTunnelNotFound, err)
}

func TestRelayManager_HandleClose(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 1024)
				for {
					_, err := c.Read(buf)
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	manager := NewRelayManager(logger, nil)
	err = manager.HandleConnect("tun_close", port)
	require.NoError(t, err)
	assert.Equal(t, 1, manager.GetActiveCount())

	err = manager.HandleClose("tun_close")
	require.NoError(t, err)
	assert.Equal(t, 0, manager.GetActiveCount())
}

func TestRelayManager_HandleCloseNotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewRelayManager(logger, nil)

	err := manager.HandleClose("non_existent")
	assert.Error(t, err)
	assert.Equal(t, ErrTunnelNotFound, err)
}

func TestRelayManager_CloseAll(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start multiple test servers
	var listeners []net.Listener
	var ports []int
	for i := 0; i < 3; i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners = append(listeners, listener)

		_, portStr, _ := net.SplitHostPort(listener.Addr().String())
		var port int
		for j := 0; j < len(portStr); j++ {
			port = port*10 + int(portStr[j]-'0')
		}
		ports = append(ports, port)

		go func(l net.Listener) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer func() { _ = c.Close() }()
					buf := make([]byte, 1024)
					for {
						_, err := c.Read(buf)
						if err != nil {
							return
						}
					}
				}(conn)
			}
		}(listener)
	}
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()

	manager := NewRelayManager(logger, nil)
	for i, port := range ports {
		err := manager.HandleConnect("tun_"+string(rune('a'+i)), port)
		require.NoError(t, err)
	}

	assert.Equal(t, 3, manager.GetActiveCount())

	manager.CloseAll()
	assert.Equal(t, 0, manager.GetActiveCount())
}

func TestRelayManager_HandleFrame(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var pongReceived bool
	var mu sync.Mutex
	sendFn := func(f *Frame) error {
		mu.Lock()
		defer mu.Unlock()
		if f.Type == FrameTypePong {
			pongReceived = true
		}
		return nil
	}

	manager := NewRelayManager(logger, sendFn)

	t.Run("ping frame sends pong", func(t *testing.T) {
		pongReceived = false
		err := manager.HandleFrame(&Frame{
			Type:     FrameTypePing,
			TunnelID: "tun_ping",
		})
		require.NoError(t, err)

		mu.Lock()
		assert.True(t, pongReceived)
		mu.Unlock()
	})

	t.Run("close frame for non-existent tunnel", func(t *testing.T) {
		err := manager.HandleFrame(&Frame{
			Type:     FrameTypeClose,
			TunnelID: "non_existent",
		})
		assert.Error(t, err)
		assert.Equal(t, ErrTunnelNotFound, err)
	})

	t.Run("data frame for non-existent tunnel", func(t *testing.T) {
		err := manager.HandleFrame(&Frame{
			Type:     FrameTypeData,
			TunnelID: "non_existent",
			Payload:  []byte("test"),
		})
		assert.Error(t, err)
		assert.Equal(t, ErrTunnelNotFound, err)
	})

	t.Run("unknown frame type", func(t *testing.T) {
		err := manager.HandleFrame(&Frame{
			Type:     0xFF,
			TunnelID: "tun_unknown",
		})
		// Unknown frames are logged but not errors
		assert.NoError(t, err)
	})
}

func TestRelayManager_ConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 1024)
				for {
					_, err := c.Read(buf)
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	manager := NewRelayManager(logger, nil)

	// Concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tunnelID := "tun_concurrent_" + string(rune('a'+idx%26))

			// Try to connect
			_ = manager.HandleConnect(tunnelID, port)

			// Try to send data
			_ = manager.HandleData(tunnelID, []byte("test"))

			// Try to close
			_ = manager.HandleClose(tunnelID)
		}(i)
	}
	wg.Wait()

	// All should be closed
	assert.Equal(t, 0, manager.GetActiveCount())
}

func TestRelayConnection_ReadFromLocal_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start a test server that doesn't send any data
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// Don't send anything, just keep connection open
		defer func() { _ = conn.Close() }()
		time.Sleep(10 * time.Second)
	}()

	relay := NewRelayConnection("tun_timeout", listener.Addr().String(), logger)
	err = relay.Connect()
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		_ = relay.ReadFromLocal(func(data []byte) error {
			return nil
		})
		close(done)
	}()

	// Give the read goroutine time to start
	time.Sleep(50 * time.Millisecond)

	// Close the relay - this should cause ReadFromLocal to return
	_ = relay.Close()

	select {
	case <-done:
		// Expected - connection was closed
	case <-time.After(time.Second):
		t.Fatal("ReadFromLocal did not return after connection close")
	}
}
