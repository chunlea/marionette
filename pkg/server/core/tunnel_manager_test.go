package core

import (
	"context"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/cryptoutil"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

func TestTunnelManager_Create(t *testing.T) {
	st := newTestStore()
	defer func() { _ = st.Close() }()

	// Create a session for the tunnel
	session := createTestSession(t, st)

	mgr := NewTunnelManager(st, zap.NewNop())

	tests := []struct {
		name    string
		input   *CreateTunnelInput
		wantErr bool
	}{
		{
			name: "browser tunnel",
			input: &CreateTunnelInput{
				SessionID: session.ID,
				Type:      "browser",
				LocalPort: 9222,
				ExpiresIn: time.Hour,
			},
			wantErr: false,
		},
		{
			name: "desktop tunnel",
			input: &CreateTunnelInput{
				SessionID: session.ID,
				Type:      "desktop",
				LocalPort: 5900,
			},
			wantErr: false,
		},
		{
			name: "http tunnel (outbound)",
			input: &CreateTunnelInput{
				SessionID: session.ID,
				Type:      "http",
				LocalPort: 8080,
			},
			wantErr: false,
		},
		{
			name: "missing session_id",
			input: &CreateTunnelInput{
				Type:      "browser",
				LocalPort: 9222,
			},
			wantErr: true,
		},
		{
			name: "missing type",
			input: &CreateTunnelInput{
				SessionID: session.ID,
				LocalPort: 9222,
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			input: &CreateTunnelInput{
				SessionID: session.ID,
				Type:      "invalid",
				LocalPort: 9222,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mgr.Create(context.Background(), tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			// Verify result
			if result.Tunnel == nil {
				t.Fatal("Create() returned nil tunnel")
			}
			if result.RawToken == "" {
				t.Error("Create() returned empty raw token")
			}
			if result.Tunnel.ID == "" {
				t.Error("tunnel ID is empty")
			}
			if result.Tunnel.SessionID != tt.input.SessionID {
				t.Errorf("SessionID = %v, want %v", result.Tunnel.SessionID, tt.input.SessionID)
			}
			if result.Tunnel.Type != tt.input.Type {
				t.Errorf("Type = %v, want %v", result.Tunnel.Type, tt.input.Type)
			}
			if result.Tunnel.LocalPort != tt.input.LocalPort {
				t.Errorf("LocalPort = %v, want %v", result.Tunnel.LocalPort, tt.input.LocalPort)
			}

			// Check direction
			expectedDirection := "inbound"
			if tt.input.Type == "http" || tt.input.Type == "tcp" {
				expectedDirection = "outbound"
			}
			if result.Tunnel.Direction != expectedDirection {
				t.Errorf("Direction = %v, want %v", result.Tunnel.Direction, expectedDirection)
			}

			// Verify token format
			if !cryptoutil.ValidateTokenFormat(result.RawToken, cryptoutil.PrefixTunnelToken) {
				t.Errorf("invalid token format: %s", result.RawToken)
			}
		})
	}
}

func TestTunnelManager_Get(t *testing.T) {
	st := newTestStore()
	defer func() { _ = st.Close() }()

	session := createTestSession(t, st)
	mgr := NewTunnelManager(st, zap.NewNop())

	// Create a tunnel
	result, err := mgr.Create(context.Background(), &CreateTunnelInput{
		SessionID: session.ID,
		Type:      "browser",
		LocalPort: 9222,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Get the tunnel
	tunnel, err := mgr.Get(context.Background(), result.Tunnel.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if tunnel.ID != result.Tunnel.ID {
		t.Errorf("ID = %v, want %v", tunnel.ID, result.Tunnel.ID)
	}

	// Get non-existent tunnel
	_, err = mgr.Get(context.Background(), "tun_nonexistent")
	if err != store.ErrNotFound {
		t.Errorf("Get() for non-existent tunnel error = %v, want ErrNotFound", err)
	}
}

func TestTunnelManager_List(t *testing.T) {
	st := newTestStore()
	defer func() { _ = st.Close() }()

	session := createTestSession(t, st)
	mgr := NewTunnelManager(st, zap.NewNop())

	// Create multiple tunnels
	for i := 0; i < 3; i++ {
		_, err := mgr.Create(context.Background(), &CreateTunnelInput{
			SessionID: session.ID,
			Type:      "browser",
			LocalPort: 9222 + i,
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// List all tunnels
	result, err := mgr.List(context.Background(), store.ListTunnelsOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result.Items) < 3 {
		t.Errorf("List() returned %d items, want at least 3", len(result.Items))
	}

	// List by session
	result, err = mgr.List(context.Background(), store.ListTunnelsOptions{
		SessionID: &session.ID,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result.Items) != 3 {
		t.Errorf("List() by session returned %d items, want 3", len(result.Items))
	}
}

func TestTunnelManager_Close(t *testing.T) {
	st := newTestStore()
	defer func() { _ = st.Close() }()

	session := createTestSession(t, st)
	mgr := NewTunnelManager(st, zap.NewNop())

	// Create a tunnel
	result, err := mgr.Create(context.Background(), &CreateTunnelInput{
		SessionID: session.ID,
		Type:      "browser",
		LocalPort: 9222,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Close the tunnel
	err = mgr.Close(context.Background(), result.Tunnel.ID)
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify tunnel is closed
	tunnel, err := mgr.Get(context.Background(), result.Tunnel.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if tunnel.ClosedAt == nil {
		t.Error("tunnel.ClosedAt is nil after Close()")
	}
}

func TestTunnelManager_ValidateToken(t *testing.T) {
	st := newTestStore()
	defer func() { _ = st.Close() }()

	session := createTestSession(t, st)
	mgr := NewTunnelManager(st, zap.NewNop())

	// Create a tunnel
	result, err := mgr.Create(context.Background(), &CreateTunnelInput{
		SessionID: session.ID,
		Type:      "browser",
		LocalPort: 9222,
		ExpiresIn: time.Hour,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Validate with correct token
	tunnel, err := mgr.ValidateToken(context.Background(), result.RawToken)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if tunnel.ID != result.Tunnel.ID {
		t.Errorf("ValidateToken() returned wrong tunnel")
	}

	// Validate with invalid token
	_, err = mgr.ValidateToken(context.Background(), "ttok_invalid")
	if err == nil {
		t.Error("ValidateToken() with invalid token should fail")
	}

	// Validate with wrong format
	_, err = mgr.ValidateToken(context.Background(), "invalid_format")
	if err != store.ErrNotFound {
		t.Errorf("ValidateToken() with wrong format error = %v, want ErrNotFound", err)
	}

	// Close tunnel and validate
	err = mgr.Close(context.Background(), result.Tunnel.ID)
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err = mgr.ValidateToken(context.Background(), result.RawToken)
	if err == nil {
		t.Error("ValidateToken() with closed tunnel should fail")
	}
}

func TestTunnelManager_ValidateToken_Expired(t *testing.T) {
	st := newTestStore()
	defer func() { _ = st.Close() }()

	session := createTestSession(t, st)
	mgr := NewTunnelManager(st, zap.NewNop())

	// Create a tunnel that expires immediately
	result, err := mgr.Create(context.Background(), &CreateTunnelInput{
		SessionID: session.ID,
		Type:      "browser",
		LocalPort: 9222,
		ExpiresIn: -time.Hour, // Already expired
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Validate should fail due to expiration
	_, err = mgr.ValidateToken(context.Background(), result.RawToken)
	if err == nil {
		t.Error("ValidateToken() with expired tunnel should fail")
	}
}

func TestTunnelManager_ConnectionTracking(t *testing.T) {
	st := newTestStore()
	defer func() { _ = st.Close() }()

	session := createTestSession(t, st)
	mgr := NewTunnelManager(st, zap.NewNop())

	// Create a tunnel
	result, err := mgr.Create(context.Background(), &CreateTunnelInput{
		SessionID: session.ID,
		Type:      "browser",
		LocalPort: 9222,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	tunnelID := result.Tunnel.ID

	// Initially not connected
	if mgr.IsConnected(tunnelID) {
		t.Error("IsConnected() should return false initially")
	}

	// Mark connected
	mgr.MarkConnected(tunnelID, "runner-1", session.ID)

	if !mgr.IsConnected(tunnelID) {
		t.Error("IsConnected() should return true after MarkConnected()")
	}

	// Get connection
	conn := mgr.GetConnection(tunnelID)
	if conn == nil {
		t.Fatal("GetConnection() returned nil")
	}
	if conn.RunnerID != "runner-1" {
		t.Errorf("RunnerID = %v, want runner-1", conn.RunnerID)
	}

	// List active connections
	connections := mgr.ListActiveConnections()
	if len(connections) != 1 {
		t.Errorf("ListActiveConnections() returned %d, want 1", len(connections))
	}

	// Mark disconnected
	mgr.MarkDisconnected(tunnelID)

	if mgr.IsConnected(tunnelID) {
		t.Error("IsConnected() should return false after MarkDisconnected()")
	}
}

func TestTunnelManager_UpdateRunner(t *testing.T) {
	st := newTestStore()
	defer func() { _ = st.Close() }()

	session := createTestSession(t, st)
	mgr := NewTunnelManager(st, zap.NewNop())

	// Create a tunnel without runner
	result, err := mgr.Create(context.Background(), &CreateTunnelInput{
		SessionID: session.ID,
		Type:      "browser",
		LocalPort: 9222,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Update runner
	runnerID := "runner-1"
	err = mgr.UpdateRunner(context.Background(), result.Tunnel.ID, &runnerID)
	if err != nil {
		t.Fatalf("UpdateRunner() error = %v", err)
	}

	// Verify update
	tunnel, err := mgr.Get(context.Background(), result.Tunnel.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if tunnel.RunnerID == nil || *tunnel.RunnerID != runnerID {
		t.Errorf("RunnerID = %v, want %v", tunnel.RunnerID, runnerID)
	}
}

func TestTunnelManager_SetPublicURL(t *testing.T) {
	st := newTestStore()
	defer func() { _ = st.Close() }()

	session := createTestSession(t, st)
	mgr := NewTunnelManager(st, zap.NewNop())

	// Create an http tunnel
	result, err := mgr.Create(context.Background(), &CreateTunnelInput{
		SessionID: session.ID,
		Type:      "http",
		LocalPort: 8080,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Set public URL
	publicURL := "https://example.com/tunnel/abc123"
	err = mgr.SetPublicURL(context.Background(), result.Tunnel.ID, publicURL)
	if err != nil {
		t.Fatalf("SetPublicURL() error = %v", err)
	}

	// Verify update
	tunnel, err := mgr.Get(context.Background(), result.Tunnel.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if tunnel.PublicURL == nil || *tunnel.PublicURL != publicURL {
		t.Errorf("PublicURL = %v, want %v", tunnel.PublicURL, publicURL)
	}
}

// Helper to create a test session
func createTestSession(t *testing.T, st store.Store) *store.Session {
	t.Helper()

	// Create workspace first
	ws := &store.Workspace{
		ID:   "ws_test123",
		Name: "test-workspace",
	}
	if err := st.CreateWorkspace(context.Background(), ws); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	// Create session
	session := &store.Session{
		ID:          "sess_test123",
		WorkspaceID: ws.ID,
		Agent:       "claude",
		Status:      "active",
	}
	if err := st.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	return session
}
