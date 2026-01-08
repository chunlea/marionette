// Package tunnel provides HTTP/TCP tunneling support for Marionette.
//
// Tunneling allows users to access services running inside agent containers
// (e.g., a web server on localhost:3000) from outside the container.
//
// Architecture:
//
//	                   ┌─────────────────────────────────────────────────────────┐
//	                   │                         Server                          │
//	                   │  ┌───────────────────────────────────────────────────┐  │
//	                   │  │                   TunnelManager                   │  │
//	                   │  │                                                   │  │
//	User ──HTTP/WS──►  │  │  ┌──────────┐       ┌───────────────────────┐     │  │
//	                   │  │  │  Proxy   │──────►│   RunnerConnection    │     │  │
//	                   │  │  └──────────┘       └───────────────────────┘     │  │
//	                   │  │                              │                    │  │
//	                   │  └──────────────────────────────│────────────────────┘  │
//	                   └─────────────────────────────────│───────────────────────┘
//	                                                     │
//	                                                     │ WebSocket
//	                                                     │
//	                   ┌─────────────────────────────────▼───────────────────────┐
//	                   │                         Agent                           │
//	                   │  ┌───────────────────────────────────────────────────┐  │
//	                   │  │                   TunnelClient                    │  │
//	                   │  │  ┌──────────┐       ┌───────────────────────┐     │  │
//	                   │  │  │  Relay   │──────►│   LocalService:port   │     │  │
//	                   │  │  └──────────┘       └───────────────────────┘     │  │
//	                   │  └───────────────────────────────────────────────────┘  │
//	                   └─────────────────────────────────────────────────────────┘
package tunnel

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Tunnel type constants.
const (
	TypeHTTP    = "http"
	TypeTCP     = "tcp"
	TypeDesktop = "desktop"
	TypeBrowser = "browser"
	TypeIOS     = "ios"
	TypeAndroid = "android"
)

// Direction constants for network policy enforcement.
const (
	// DirectionInbound: Server/User -> Agent (viewing agent's screen/browser)
	DirectionInbound = "inbound"
	// DirectionOutbound: Agent -> External (exposing agent's port to internet)
	DirectionOutbound = "outbound"
)

// Status constants for tunnel state.
const (
	StatusPending = "pending" // Created but not yet connected
	StatusActive  = "active"  // Connected and proxying traffic
	StatusClosed  = "closed"  // Explicitly closed
	StatusExpired = "expired" // Expired due to TTL
)

// Common errors.
var (
	ErrTunnelNotFound     = errors.New("tunnel not found")
	ErrTunnelClosed       = errors.New("tunnel is closed")
	ErrTunnelExpired      = errors.New("tunnel has expired")
	ErrInvalidTunnelType  = errors.New("invalid tunnel type")
	ErrInvalidDirection   = errors.New("invalid direction for tunnel type")
	ErrRunnerNotConnected = errors.New("runner not connected")
	ErrInvalidToken       = errors.New("invalid tunnel token")
	ErrPortBlocked        = errors.New("port is blocked for security")
	ErrSSRFBlocked        = errors.New("request blocked due to SSRF protection")
	ErrConnectionFailed   = errors.New("failed to connect to local service")
)

// Tunnel represents an active tunnel instance.
type Tunnel struct {
	ID        string
	SessionID string
	RunnerID  string
	Type      string
	Direction string
	LocalPort int
	PublicURL string
	Token     string // Only set during creation, not stored
	ExpiresAt time.Time
	CreatedAt time.Time
	ClosedAt  *time.Time
}

// IsExpired returns true if the tunnel has expired.
func (t *Tunnel) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// IsClosed returns true if the tunnel is closed.
func (t *Tunnel) IsClosed() bool {
	return t.ClosedAt != nil
}

// IsActive returns true if the tunnel is active (not closed and not expired).
func (t *Tunnel) IsActive() bool {
	return !t.IsClosed() && !t.IsExpired()
}

// CreateTunnelOptions defines options for creating a new tunnel.
type CreateTunnelOptions struct {
	SessionID string        // Required: session this tunnel belongs to
	RunnerID  string        // Required: runner that will handle the tunnel
	Type      string        // Required: tunnel type (http, tcp, etc.)
	LocalPort int           // Required: local port in the container
	TTL       time.Duration // Optional: time-to-live (default: 1 hour)
}

// Validate validates the create options.
func (o *CreateTunnelOptions) Validate() error {
	if o.SessionID == "" {
		return errors.New("session_id is required")
	}
	if o.RunnerID == "" {
		return errors.New("runner_id is required")
	}
	if o.LocalPort <= 0 || o.LocalPort > 65535 {
		return errors.New("invalid local_port: must be between 1 and 65535")
	}

	// Validate type and direction
	switch o.Type {
	case TypeHTTP, TypeTCP:
		// Outbound types - valid
	case TypeDesktop, TypeBrowser, TypeIOS, TypeAndroid:
		// Inbound types - valid
	default:
		return ErrInvalidTunnelType
	}

	return nil
}

// Direction returns the direction based on tunnel type.
func (o *CreateTunnelOptions) Direction() string {
	switch o.Type {
	case TypeDesktop, TypeBrowser, TypeIOS, TypeAndroid:
		return DirectionInbound
	default:
		return DirectionOutbound
	}
}

// Manager defines the interface for tunnel management.
type Manager interface {
	// Create creates a new tunnel and returns the tunnel info with token.
	Create(ctx context.Context, opts CreateTunnelOptions) (*Tunnel, error)

	// Get retrieves a tunnel by ID.
	Get(ctx context.Context, tunnelID string) (*Tunnel, error)

	// GetBySession returns all active tunnels for a session.
	GetBySession(ctx context.Context, sessionID string) ([]*Tunnel, error)

	// Close closes a tunnel.
	Close(ctx context.Context, tunnelID string) error

	// CloseBySession closes all tunnels for a session.
	CloseBySession(ctx context.Context, sessionID string) error

	// ValidateToken validates a tunnel token and returns the tunnel if valid.
	ValidateToken(ctx context.Context, tunnelID, token string) (*Tunnel, error)

	// HandleHTTPRequest handles an incoming HTTP request for a tunnel.
	HandleHTTPRequest(ctx context.Context, tunnelID string, w http.ResponseWriter, r *http.Request) error

	// HandleTCPConnection handles an incoming TCP connection for a tunnel.
	// The connection should already be upgraded (e.g., from WebSocket).
	HandleTCPConnection(ctx context.Context, tunnelID string, conn Connection) error
}

// Connection represents a bidirectional byte stream.
// This interface abstracts WebSocket, raw TCP, or other connection types.
type Connection interface {
	// Read reads data from the connection.
	Read(p []byte) (n int, err error)

	// Write writes data to the connection.
	Write(p []byte) (n int, err error)

	// Close closes the connection.
	Close() error

	// SetDeadline sets the read and write deadlines.
	SetDeadline(t time.Time) error

	// SetReadDeadline sets the read deadline.
	SetReadDeadline(t time.Time) error

	// SetWriteDeadline sets the write deadline.
	SetWriteDeadline(t time.Time) error
}

// Relay defines the interface for relaying traffic between connections.
// This is implemented by the agent to forward traffic to local services.
type Relay interface {
	// Start starts the relay for the given tunnel.
	Start(ctx context.Context, tunnel *Tunnel) error

	// Stop stops the relay for the given tunnel.
	Stop(tunnelID string) error

	// IsRunning returns true if the relay is running for the tunnel.
	IsRunning(tunnelID string) bool
}

// Store defines the interface for tunnel persistence.
type Store interface {
	// CreateTunnel stores a new tunnel.
	CreateTunnel(ctx context.Context, tunnel *Tunnel, tokenHash string, hashVersion int) error

	// GetTunnel retrieves a tunnel by ID.
	GetTunnel(ctx context.Context, id string) (*Tunnel, error)

	// GetTunnelByHash retrieves a tunnel by token hash.
	GetTunnelByHash(ctx context.Context, hash string) (*Tunnel, error)

	// ListTunnels returns tunnels matching the given criteria.
	ListTunnels(ctx context.Context, opts ListOptions) ([]*Tunnel, error)

	// UpdateTunnel updates a tunnel (e.g., to set ClosedAt).
	UpdateTunnel(ctx context.Context, id string, updates Updates) error

	// DeleteExpiredTunnels removes tunnels that have expired.
	DeleteExpiredTunnels(ctx context.Context) (int64, error)
}

// ListOptions defines options for listing tunnels.
type ListOptions struct {
	SessionID     string   // Filter by session
	RunnerID      string   // Filter by runner
	Types         []string // Filter by type
	IncludeClosed bool     // Include closed tunnels
}

// Updates defines fields that can be updated on a tunnel.
type Updates struct {
	PublicURL *string
	ClosedAt  *time.Time
}

// ValidTunnelTypes returns a list of valid tunnel types.
func ValidTunnelTypes() []string {
	return []string{TypeHTTP, TypeTCP, TypeDesktop, TypeBrowser, TypeIOS, TypeAndroid}
}

// IsValidTunnelType checks if the given type is a valid tunnel type.
func IsValidTunnelType(t string) bool {
	for _, valid := range ValidTunnelTypes() {
		if t == valid {
			return true
		}
	}
	return false
}

// OutboundTypes returns tunnel types that expose ports to the external network.
func OutboundTypes() []string {
	return []string{TypeHTTP, TypeTCP}
}

// InboundTypes returns tunnel types that stream to the user.
func InboundTypes() []string {
	return []string{TypeDesktop, TypeBrowser, TypeIOS, TypeAndroid}
}
