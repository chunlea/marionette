package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/cryptoutil"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// TunnelManager handles tunnel lifecycle management.
type TunnelManager struct {
	store  store.Store
	logger *zap.Logger
	mu     sync.RWMutex

	// Active tunnel connections (tunnelID -> connection state)
	// This tracks which tunnels have active gRPC streams
	activeConnections map[string]*TunnelConnection
}

// TunnelConnection tracks the connection state for a tunnel.
type TunnelConnection struct {
	TunnelID  string
	RunnerID  string
	SessionID string
	Connected bool
	ConnectAt time.Time
}

// TunnelInfo contains essential tunnel information for WebSocket handlers.
type TunnelInfo struct {
	TunnelID  string
	SessionID string
	Type      string
}

// NewTunnelManager creates a new TunnelManager.
func NewTunnelManager(st store.Store, logger *zap.Logger) *TunnelManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TunnelManager{
		store:             st,
		logger:            logger,
		activeConnections: make(map[string]*TunnelConnection),
	}
}

// CreateTunnelInput contains input for creating a tunnel.
type CreateTunnelInput struct {
	SessionID string
	RunnerID  *string
	Type      string // browser, desktop, http, tcp, ios, android
	LocalPort int
	ExpiresIn time.Duration // How long until the tunnel expires
	TenantID  *string
}

// CreateTunnelResult contains the result of creating a tunnel.
type CreateTunnelResult struct {
	Tunnel   *store.Tunnel
	RawToken string // The raw token (only returned once)
}

// Create creates a new tunnel with a generated token.
func (m *TunnelManager) Create(ctx context.Context, input *CreateTunnelInput) (*CreateTunnelResult, error) {
	if input.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if input.Type == "" {
		return nil, fmt.Errorf("type is required")
	}

	// Validate tunnel type
	validTypes := map[string]bool{
		"browser": true,
		"desktop": true,
		"http":    true,
		"tcp":     true,
		"ios":     true,
		"android": true,
	}
	if !validTypes[input.Type] {
		return nil, fmt.Errorf("invalid tunnel type: %s", input.Type)
	}

	// Determine direction based on type
	// inbound: streaming to user (browser, desktop, ios, android)
	// outbound: exposing to internet (http, tcp)
	direction := "inbound"
	if input.Type == "http" || input.Type == "tcp" {
		direction = "outbound"
	}

	// Generate token
	rawToken, tokenPrefix, tokenHash, hashVersion, err := cryptoutil.GenerateTunnelToken()
	if err != nil {
		return nil, fmt.Errorf("generating token: %w", err)
	}

	// Default expiration to 24 hours
	expiresIn := input.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 24 * time.Hour
	}

	now := time.Now()
	tunnel := &store.Tunnel{
		ID:          id.Tunnel(),
		SessionID:   input.SessionID,
		RunnerID:    input.RunnerID,
		Type:        input.Type,
		Direction:   direction,
		LocalPort:   input.LocalPort,
		TokenHash:   tokenHash,
		TokenPrefix: tokenPrefix,
		HashVersion: hashVersion,
		TenantID:    input.TenantID,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   now.Add(expiresIn),
	}

	if err := m.store.CreateTunnel(ctx, tunnel); err != nil {
		return nil, fmt.Errorf("storing tunnel: %w", err)
	}

	m.logger.Info("tunnel created",
		zap.String("tunnel_id", tunnel.ID),
		zap.String("session_id", tunnel.SessionID),
		zap.String("type", tunnel.Type),
		zap.String("token_prefix", tokenPrefix),
	)

	return &CreateTunnelResult{
		Tunnel:   tunnel,
		RawToken: rawToken,
	}, nil
}

// Get retrieves a tunnel by ID.
func (m *TunnelManager) Get(ctx context.Context, tunnelID string) (*store.Tunnel, error) {
	tunnel, err := m.store.GetTunnel(ctx, tunnelID)
	if err != nil {
		return nil, err
	}
	return tunnel, nil
}

// List retrieves tunnels with optional filters.
func (m *TunnelManager) List(ctx context.Context, opts store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	return m.store.ListTunnels(ctx, opts)
}

// Close closes a tunnel.
func (m *TunnelManager) Close(ctx context.Context, tunnelID string) error {
	now := time.Now()
	err := m.store.UpdateTunnel(ctx, tunnelID, store.TunnelUpdates{
		ClosedAt: &now,
	})
	if err != nil {
		return err
	}

	// Remove from active connections
	m.mu.Lock()
	delete(m.activeConnections, tunnelID)
	m.mu.Unlock()

	m.logger.Info("tunnel closed", zap.String("tunnel_id", tunnelID))
	return nil
}

// ValidateToken validates a tunnel token and returns the tunnel.
// Returns store.ErrNotFound if the token is invalid or tunnel doesn't exist.
func (m *TunnelManager) ValidateToken(ctx context.Context, token string) (*store.Tunnel, error) {
	// Validate token format
	if !cryptoutil.ValidateTokenFormat(token, cryptoutil.PrefixTunnelToken) {
		return nil, store.ErrNotFound
	}

	// Hash the token
	hash := cryptoutil.HashToken(token)

	// Look up by hash
	tunnel, err := m.store.GetTunnelByTokenHash(ctx, hash)
	if err != nil {
		return nil, err
	}

	// Check if tunnel is closed
	if tunnel.ClosedAt != nil {
		return nil, fmt.Errorf("tunnel is closed")
	}

	// Check if tunnel is expired
	if time.Now().After(tunnel.ExpiresAt) {
		return nil, fmt.Errorf("tunnel has expired")
	}

	return tunnel, nil
}

// UpdateRunner updates the runner ID for a tunnel.
func (m *TunnelManager) UpdateRunner(ctx context.Context, tunnelID string, runnerID *string) error {
	return m.store.UpdateTunnel(ctx, tunnelID, store.TunnelUpdates{
		RunnerID: runnerID,
	})
}

// SetPublicURL sets the public URL for a tunnel (for outbound tunnels).
func (m *TunnelManager) SetPublicURL(ctx context.Context, tunnelID string, publicURL string) error {
	return m.store.UpdateTunnel(ctx, tunnelID, store.TunnelUpdates{
		PublicURL: &publicURL,
	})
}

// MarkConnected marks a tunnel as having an active connection.
func (m *TunnelManager) MarkConnected(tunnelID, runnerID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.activeConnections[tunnelID] = &TunnelConnection{
		TunnelID:  tunnelID,
		RunnerID:  runnerID,
		SessionID: sessionID,
		Connected: true,
		ConnectAt: time.Now(),
	}

	m.logger.Debug("tunnel connected",
		zap.String("tunnel_id", tunnelID),
		zap.String("runner_id", runnerID),
	)
}

// MarkDisconnected marks a tunnel as disconnected.
func (m *TunnelManager) MarkDisconnected(tunnelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.activeConnections, tunnelID)

	m.logger.Debug("tunnel disconnected", zap.String("tunnel_id", tunnelID))
}

// IsConnected checks if a tunnel has an active connection.
func (m *TunnelManager) IsConnected(tunnelID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.activeConnections[tunnelID]
	return ok && conn.Connected
}

// GetConnection returns the connection state for a tunnel.
func (m *TunnelManager) GetConnection(tunnelID string) *TunnelConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if conn, ok := m.activeConnections[tunnelID]; ok {
		// Return a copy
		c := *conn
		return &c
	}
	return nil
}

// ListActiveConnections returns all active tunnel connections.
func (m *TunnelManager) ListActiveConnections() []*TunnelConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*TunnelConnection, 0, len(m.activeConnections))
	for _, conn := range m.activeConnections {
		c := *conn
		result = append(result, &c)
	}
	return result
}

// CleanupExpired closes tunnels that have expired.
// This should be called periodically by a background job.
func (m *TunnelManager) CleanupExpired(ctx context.Context) (int, error) {
	// List active (non-closed) tunnels
	result, err := m.store.ListTunnels(ctx, store.ListTunnelsOptions{
		IncludeClosed: false,
	})
	if err != nil {
		return 0, err
	}

	var closed int
	now := time.Now()
	for _, tunnel := range result.Items {
		if now.After(tunnel.ExpiresAt) {
			if err := m.Close(ctx, tunnel.ID); err != nil {
				m.logger.Warn("failed to close expired tunnel",
					zap.String("tunnel_id", tunnel.ID),
					zap.Error(err),
				)
				continue
			}
			closed++
		}
	}

	if closed > 0 {
		m.logger.Info("cleaned up expired tunnels", zap.Int("count", closed))
	}

	return closed, nil
}
