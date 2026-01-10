package tunnel

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/cryptoutil"
	"github.com/chunlea/marionette/pkg/id"
	"go.uber.org/zap"
)

// DefaultTTL is the default tunnel time-to-live.
const DefaultTTL = 1 * time.Hour

// MaxTTL is the maximum tunnel time-to-live.
const MaxTTL = 24 * time.Hour

// TunnelManager manages tunnel lifecycle and routing.
type TunnelManager struct {
	store  Store
	logger *zap.Logger

	// In-memory cache of active tunnels for fast lookup
	tunnels   map[string]*activeTunnel
	tunnelsMu sync.RWMutex

	// Connection handlers by runner ID
	handlers   map[string]ConnectionHandler
	handlersMu sync.RWMutex

	// ID generator (injectable for testing)
	idGen func() string

	// URL generator for public URLs
	urlGen URLGenerator

	// Options
	baseURL string
}

// activeTunnel holds runtime state for an active tunnel.
type activeTunnel struct {
	*Tunnel
	tokenHash   string
	hashVersion int
	connections sync.WaitGroup
}

// ConnectionHandler handles tunnel connections to a specific runner.
type ConnectionHandler interface {
	// SendTunnelData sends data through the tunnel to the runner.
	SendTunnelData(ctx context.Context, tunnelID string, data []byte) error

	// ReceiveTunnelData receives data from the tunnel from the runner.
	ReceiveTunnelData(ctx context.Context, tunnelID string) ([]byte, error)

	// CloseTunnel notifies the runner to close the tunnel.
	CloseTunnel(ctx context.Context, tunnelID string) error

	// IsConnected returns true if the runner is connected.
	IsConnected() bool
}

// URLGenerator generates public URLs for tunnels.
type URLGenerator interface {
	// GenerateURL generates a public URL for the given tunnel.
	GenerateURL(tunnel *Tunnel) string
}

// ManagerConfig holds configuration for TunnelManager.
type ManagerConfig struct {
	Store   Store
	Logger  *zap.Logger
	BaseURL string // Base URL for generating public URLs
	IDGen   func() string
	URLGen  URLGenerator
}

// ManagerOption is a functional option for configuring TunnelManager.
type ManagerOption func(*TunnelManager)

// WithStore sets the tunnel store.
func WithStore(store Store) ManagerOption {
	return func(m *TunnelManager) {
		m.store = store
	}
}

// WithLogger sets the logger.
func WithLogger(logger *zap.Logger) ManagerOption {
	return func(m *TunnelManager) {
		m.logger = logger
	}
}

// WithBaseURL sets the base URL for public URLs.
func WithBaseURL(baseURL string) ManagerOption {
	return func(m *TunnelManager) {
		m.baseURL = baseURL
	}
}

// WithIDGen sets the ID generator function.
func WithIDGen(idGen func() string) ManagerOption {
	return func(m *TunnelManager) {
		m.idGen = idGen
	}
}

// WithURLGenerator sets the URL generator.
func WithURLGenerator(urlGen URLGenerator) ManagerOption {
	return func(m *TunnelManager) {
		m.urlGen = urlGen
	}
}

// NewTunnelManager creates a new TunnelManager.
func NewTunnelManager(opts ...ManagerOption) *TunnelManager {
	m := &TunnelManager{
		logger:   zap.NewNop(),
		tunnels:  make(map[string]*activeTunnel),
		handlers: make(map[string]ConnectionHandler),
		idGen:    id.Tunnel,
		baseURL:  "http://localhost:8080",
	}

	for _, opt := range opts {
		opt(m)
	}

	if m.urlGen == nil {
		m.urlGen = &defaultURLGenerator{baseURL: m.baseURL}
	}

	return m
}

// defaultURLGenerator generates simple path-based URLs.
type defaultURLGenerator struct {
	baseURL string
}

func (g *defaultURLGenerator) GenerateURL(tunnel *Tunnel) string {
	return fmt.Sprintf("%s/tunnels/%s", g.baseURL, tunnel.ID)
}

// Create creates a new tunnel.
func (m *TunnelManager) Create(ctx context.Context, opts CreateTunnelOptions) (*Tunnel, error) {
	// Validate options
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	// Set default TTL
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		ttl = MaxTTL
	}

	// Generate token
	token, displayPrefix, hash, version, err := cryptoutil.GenerateTunnelToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	// Create tunnel
	now := time.Now()
	tunnel := &Tunnel{
		ID:        m.idGen(),
		SessionID: opts.SessionID,
		RunnerID:  opts.RunnerID,
		Type:      opts.Type,
		Direction: opts.Direction(),
		LocalPort: opts.LocalPort,
		Token:     token, // Only available during creation
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}

	// Generate public URL
	tunnel.PublicURL = m.urlGen.GenerateURL(tunnel)

	// Store in database
	if m.store != nil {
		if err := m.store.CreateTunnel(ctx, tunnel, hash, version); err != nil {
			return nil, fmt.Errorf("failed to store tunnel: %w", err)
		}
	}

	// Add to in-memory cache
	active := &activeTunnel{
		Tunnel:      tunnel,
		tokenHash:   hash,
		hashVersion: version,
	}
	m.tunnelsMu.Lock()
	m.tunnels[tunnel.ID] = active
	m.tunnelsMu.Unlock()

	m.logger.Info("tunnel created",
		zap.String("tunnel_id", tunnel.ID),
		zap.String("session_id", tunnel.SessionID),
		zap.String("runner_id", tunnel.RunnerID),
		zap.String("type", tunnel.Type),
		zap.Int("local_port", tunnel.LocalPort),
		zap.String("token_prefix", displayPrefix),
		zap.Time("expires_at", tunnel.ExpiresAt),
	)

	return tunnel, nil
}

// Get retrieves a tunnel by ID.
// Returns ErrTunnelClosed if the tunnel is closed, ErrTunnelExpired if expired.
func (m *TunnelManager) Get(ctx context.Context, tunnelID string) (*Tunnel, error) {
	// Check cache first
	m.tunnelsMu.RLock()
	active, exists := m.tunnels[tunnelID]
	m.tunnelsMu.RUnlock()

	if exists {
		return active.Tunnel, nil
	}

	// Fall back to store
	if m.store != nil {
		tunnel, err := m.store.GetTunnel(ctx, tunnelID)
		if err != nil {
			return nil, err
		}
		// Check if closed or expired
		if tunnel.IsClosed() {
			return nil, ErrTunnelClosed
		}
		if tunnel.IsExpired() {
			return nil, ErrTunnelExpired
		}
		return tunnel, nil
	}

	return nil, ErrTunnelNotFound
}

// GetBySession returns all active tunnels for a session.
func (m *TunnelManager) GetBySession(ctx context.Context, sessionID string) ([]*Tunnel, error) {
	// Check cache first
	var result []*Tunnel
	m.tunnelsMu.RLock()
	for _, active := range m.tunnels {
		if active.SessionID == sessionID && active.IsActive() {
			result = append(result, active.Tunnel)
		}
	}
	m.tunnelsMu.RUnlock()

	// If we have a store, merge with stored data
	if m.store != nil && len(result) == 0 {
		tunnels, err := m.store.ListTunnels(ctx, ListOptions{
			SessionID:     sessionID,
			IncludeClosed: false,
		})
		if err != nil {
			return nil, err
		}
		return tunnels, nil
	}

	return result, nil
}

// Close closes a tunnel.
func (m *TunnelManager) Close(ctx context.Context, tunnelID string) error {
	m.tunnelsMu.Lock()
	active, exists := m.tunnels[tunnelID]
	if exists {
		now := time.Now()
		active.ClosedAt = &now
		delete(m.tunnels, tunnelID)
	}
	m.tunnelsMu.Unlock()

	if !exists {
		return ErrTunnelNotFound
	}

	// Update store
	if m.store != nil {
		now := time.Now()
		if err := m.store.UpdateTunnel(ctx, tunnelID, Updates{ClosedAt: &now}); err != nil {
			m.logger.Warn("failed to update tunnel in store",
				zap.String("tunnel_id", tunnelID),
				zap.Error(err),
			)
		}
	}

	// Notify runner
	m.handlersMu.RLock()
	handler, hasHandler := m.handlers[active.RunnerID]
	m.handlersMu.RUnlock()

	if hasHandler && handler.IsConnected() {
		if err := handler.CloseTunnel(ctx, tunnelID); err != nil {
			m.logger.Warn("failed to notify runner of tunnel close",
				zap.String("tunnel_id", tunnelID),
				zap.String("runner_id", active.RunnerID),
				zap.Error(err),
			)
		}
	}

	m.logger.Info("tunnel closed",
		zap.String("tunnel_id", tunnelID),
		zap.String("session_id", active.SessionID),
	)

	return nil
}

// CloseBySession closes all tunnels for a session.
func (m *TunnelManager) CloseBySession(ctx context.Context, sessionID string) error {
	// Get tunnels for session
	var toClose []string
	m.tunnelsMu.RLock()
	for id, active := range m.tunnels {
		if active.SessionID == sessionID {
			toClose = append(toClose, id)
		}
	}
	m.tunnelsMu.RUnlock()

	// Close each tunnel
	var lastErr error
	for _, id := range toClose {
		if err := m.Close(ctx, id); err != nil && !isNotFoundError(err) {
			lastErr = err
			m.logger.Warn("failed to close tunnel",
				zap.String("tunnel_id", id),
				zap.Error(err),
			)
		}
	}

	return lastErr
}

// ValidateToken validates a tunnel token and returns the tunnel if valid.
func (m *TunnelManager) ValidateToken(_ context.Context, tunnelID, token string) (*Tunnel, error) {
	// Get tunnel from cache
	m.tunnelsMu.RLock()
	active, exists := m.tunnels[tunnelID]
	m.tunnelsMu.RUnlock()

	if !exists {
		return nil, ErrTunnelNotFound
	}

	// Check if closed or expired
	if active.IsClosed() {
		return nil, ErrTunnelClosed
	}
	if active.IsExpired() {
		return nil, ErrTunnelExpired
	}

	// Verify token
	if !cryptoutil.VerifyToken(token, active.tokenHash, active.hashVersion, nil) {
		return nil, ErrInvalidToken
	}

	return active.Tunnel, nil
}

// HandleHTTPRequest handles an incoming HTTP request for a tunnel.
func (m *TunnelManager) HandleHTTPRequest(ctx context.Context, tunnelID string, w http.ResponseWriter, r *http.Request) error {
	// Get tunnel
	m.tunnelsMu.RLock()
	active, exists := m.tunnels[tunnelID]
	m.tunnelsMu.RUnlock()

	if !exists {
		return ErrTunnelNotFound
	}

	// Check if closed or expired
	if active.IsClosed() {
		return ErrTunnelClosed
	}
	if active.IsExpired() {
		return ErrTunnelExpired
	}

	// Get handler for runner
	m.handlersMu.RLock()
	handler, hasHandler := m.handlers[active.RunnerID]
	m.handlersMu.RUnlock()

	if !hasHandler || !handler.IsConnected() {
		return ErrRunnerNotConnected
	}

	// Track active connection
	active.connections.Add(1)
	defer active.connections.Done()

	// HTTP proxying will be implemented in PR 2
	_ = ctx
	_ = w
	_ = r
	_ = handler

	return nil
}

// HandleTCPConnection handles an incoming TCP connection for a tunnel.
func (m *TunnelManager) HandleTCPConnection(ctx context.Context, tunnelID string, conn Connection) error {
	// Get tunnel
	m.tunnelsMu.RLock()
	active, exists := m.tunnels[tunnelID]
	m.tunnelsMu.RUnlock()

	if !exists {
		return ErrTunnelNotFound
	}

	// Check if closed or expired
	if active.IsClosed() {
		return ErrTunnelClosed
	}
	if active.IsExpired() {
		return ErrTunnelExpired
	}

	// Get handler for runner
	m.handlersMu.RLock()
	handler, hasHandler := m.handlers[active.RunnerID]
	m.handlersMu.RUnlock()

	if !hasHandler || !handler.IsConnected() {
		return ErrRunnerNotConnected
	}

	// Track active connection
	active.connections.Add(1)
	defer active.connections.Done()

	// TCP proxying will be implemented in PR 3
	_ = ctx
	_ = conn
	_ = handler

	return nil
}

// RegisterHandler registers a connection handler for a runner.
func (m *TunnelManager) RegisterHandler(runnerID string, handler ConnectionHandler) {
	m.handlersMu.Lock()
	m.handlers[runnerID] = handler
	m.handlersMu.Unlock()

	m.logger.Debug("handler registered",
		zap.String("runner_id", runnerID),
	)
}

// UnregisterHandler removes a connection handler for a runner.
func (m *TunnelManager) UnregisterHandler(runnerID string) {
	m.handlersMu.Lock()
	delete(m.handlers, runnerID)
	m.handlersMu.Unlock()

	m.logger.Debug("handler unregistered",
		zap.String("runner_id", runnerID),
	)
}

// GetActiveCount returns the number of active tunnels.
func (m *TunnelManager) GetActiveCount() int {
	m.tunnelsMu.RLock()
	defer m.tunnelsMu.RUnlock()

	count := 0
	for _, active := range m.tunnels {
		if active.IsActive() {
			count++
		}
	}
	return count
}

// CleanupExpired removes expired tunnels from cache.
func (m *TunnelManager) CleanupExpired(ctx context.Context) (int, error) {
	// Remove from cache
	var expired []string
	m.tunnelsMu.Lock()
	for id, active := range m.tunnels {
		if active.IsExpired() {
			expired = append(expired, id)
			delete(m.tunnels, id)
		}
	}
	m.tunnelsMu.Unlock()

	// Remove from store
	if m.store != nil {
		_, err := m.store.DeleteExpiredTunnels(ctx)
		if err != nil {
			m.logger.Warn("failed to delete expired tunnels from store",
				zap.Error(err),
			)
		}
	}

	if len(expired) > 0 {
		m.logger.Info("cleaned up expired tunnels",
			zap.Int("count", len(expired)),
		)
	}

	return len(expired), nil
}

// isNotFoundError checks if an error is a "not found" type error.
func isNotFoundError(err error) bool {
	return err == ErrTunnelNotFound
}
