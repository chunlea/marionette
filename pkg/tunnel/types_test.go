package tunnel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTunnel_IsExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{
			name:      "not expired",
			expiresAt: time.Now().Add(time.Hour),
			want:      false,
		},
		{
			name:      "expired",
			expiresAt: time.Now().Add(-time.Hour),
			want:      true,
		},
		{
			name:      "just expired",
			expiresAt: time.Now().Add(-time.Millisecond),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tunnel := &Tunnel{ExpiresAt: tt.expiresAt}
			assert.Equal(t, tt.want, tunnel.IsExpired())
		})
	}
}

func TestTunnel_IsClosed(t *testing.T) {
	tests := []struct {
		name     string
		closedAt *time.Time
		want     bool
	}{
		{
			name:     "not closed",
			closedAt: nil,
			want:     false,
		},
		{
			name:     "closed",
			closedAt: func() *time.Time { t := time.Now(); return &t }(),
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tunnel := &Tunnel{ClosedAt: tt.closedAt}
			assert.Equal(t, tt.want, tunnel.IsClosed())
		})
	}
}

func TestTunnel_IsActive(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		tunnel   *Tunnel
		expected bool
	}{
		{
			name: "active tunnel",
			tunnel: &Tunnel{
				ExpiresAt: now.Add(time.Hour),
				ClosedAt:  nil,
			},
			expected: true,
		},
		{
			name: "expired tunnel",
			tunnel: &Tunnel{
				ExpiresAt: now.Add(-time.Hour),
				ClosedAt:  nil,
			},
			expected: false,
		},
		{
			name: "closed tunnel",
			tunnel: &Tunnel{
				ExpiresAt: now.Add(time.Hour),
				ClosedAt:  &now,
			},
			expected: false,
		},
		{
			name: "closed and expired",
			tunnel: &Tunnel{
				ExpiresAt: now.Add(-time.Hour),
				ClosedAt:  &now,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.tunnel.IsActive())
		})
	}
}

func TestCreateTunnelOptions_Validate(t *testing.T) {
	tests := []struct {
		name    string
		opts    CreateTunnelOptions
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid HTTP tunnel",
			opts: CreateTunnelOptions{
				SessionID: "sess_123",
				RunnerID:  "run_456",
				Type:      TypeHTTP,
				LocalPort: 3000,
			},
			wantErr: false,
		},
		{
			name: "valid TCP tunnel",
			opts: CreateTunnelOptions{
				SessionID: "sess_123",
				RunnerID:  "run_456",
				Type:      TypeTCP,
				LocalPort: 5432,
			},
			wantErr: false,
		},
		{
			name: "valid desktop tunnel",
			opts: CreateTunnelOptions{
				SessionID: "sess_123",
				RunnerID:  "run_456",
				Type:      TypeDesktop,
				LocalPort: 5900,
			},
			wantErr: false,
		},
		{
			name: "missing session_id",
			opts: CreateTunnelOptions{
				RunnerID:  "run_456",
				Type:      TypeHTTP,
				LocalPort: 3000,
			},
			wantErr: true,
			errMsg:  "session_id is required",
		},
		{
			name: "missing runner_id",
			opts: CreateTunnelOptions{
				SessionID: "sess_123",
				Type:      TypeHTTP,
				LocalPort: 3000,
			},
			wantErr: true,
			errMsg:  "runner_id is required",
		},
		{
			name: "invalid local_port zero",
			opts: CreateTunnelOptions{
				SessionID: "sess_123",
				RunnerID:  "run_456",
				Type:      TypeHTTP,
				LocalPort: 0,
			},
			wantErr: true,
			errMsg:  "invalid local_port",
		},
		{
			name: "invalid local_port negative",
			opts: CreateTunnelOptions{
				SessionID: "sess_123",
				RunnerID:  "run_456",
				Type:      TypeHTTP,
				LocalPort: -1,
			},
			wantErr: true,
			errMsg:  "invalid local_port",
		},
		{
			name: "invalid local_port too high",
			opts: CreateTunnelOptions{
				SessionID: "sess_123",
				RunnerID:  "run_456",
				Type:      TypeHTTP,
				LocalPort: 65536,
			},
			wantErr: true,
			errMsg:  "invalid local_port",
		},
		{
			name: "invalid type",
			opts: CreateTunnelOptions{
				SessionID: "sess_123",
				RunnerID:  "run_456",
				Type:      "invalid",
				LocalPort: 3000,
			},
			wantErr: true,
			errMsg:  "invalid tunnel type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCreateTunnelOptions_Direction(t *testing.T) {
	tests := []struct {
		name     string
		typ      string
		expected string
	}{
		{
			name:     "HTTP is outbound",
			typ:      TypeHTTP,
			expected: DirectionOutbound,
		},
		{
			name:     "TCP is outbound",
			typ:      TypeTCP,
			expected: DirectionOutbound,
		},
		{
			name:     "desktop is inbound",
			typ:      TypeDesktop,
			expected: DirectionInbound,
		},
		{
			name:     "browser is inbound",
			typ:      TypeBrowser,
			expected: DirectionInbound,
		},
		{
			name:     "iOS is inbound",
			typ:      TypeIOS,
			expected: DirectionInbound,
		},
		{
			name:     "Android is inbound",
			typ:      TypeAndroid,
			expected: DirectionInbound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := CreateTunnelOptions{Type: tt.typ}
			assert.Equal(t, tt.expected, opts.Direction())
		})
	}
}

func TestValidTunnelTypes(t *testing.T) {
	types := ValidTunnelTypes()
	assert.Len(t, types, 6)
	assert.Contains(t, types, TypeHTTP)
	assert.Contains(t, types, TypeTCP)
	assert.Contains(t, types, TypeDesktop)
	assert.Contains(t, types, TypeBrowser)
	assert.Contains(t, types, TypeIOS)
	assert.Contains(t, types, TypeAndroid)
}

func TestIsValidTunnelType(t *testing.T) {
	tests := []struct {
		typ   string
		valid bool
	}{
		{TypeHTTP, true},
		{TypeTCP, true},
		{TypeDesktop, true},
		{TypeBrowser, true},
		{TypeIOS, true},
		{TypeAndroid, true},
		{"invalid", false},
		{"", false},
		{"HTTP", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			assert.Equal(t, tt.valid, IsValidTunnelType(tt.typ))
		})
	}
}

func TestOutboundTypes(t *testing.T) {
	types := OutboundTypes()
	assert.Len(t, types, 2)
	assert.Contains(t, types, TypeHTTP)
	assert.Contains(t, types, TypeTCP)
}

func TestInboundTypes(t *testing.T) {
	types := InboundTypes()
	assert.Len(t, types, 4)
	assert.Contains(t, types, TypeDesktop)
	assert.Contains(t, types, TypeBrowser)
	assert.Contains(t, types, TypeIOS)
	assert.Contains(t, types, TypeAndroid)
}

func TestConstants(t *testing.T) {
	// Verify constants have expected values
	assert.Equal(t, "http", TypeHTTP)
	assert.Equal(t, "tcp", TypeTCP)
	assert.Equal(t, "desktop", TypeDesktop)
	assert.Equal(t, "browser", TypeBrowser)
	assert.Equal(t, "ios", TypeIOS)
	assert.Equal(t, "android", TypeAndroid)

	assert.Equal(t, "inbound", DirectionInbound)
	assert.Equal(t, "outbound", DirectionOutbound)

	assert.Equal(t, "pending", StatusPending)
	assert.Equal(t, "active", StatusActive)
	assert.Equal(t, "closed", StatusClosed)
	assert.Equal(t, "expired", StatusExpired)
}

func TestErrors(t *testing.T) {
	// Verify error messages
	assert.Equal(t, "tunnel not found", ErrTunnelNotFound.Error())
	assert.Equal(t, "tunnel is closed", ErrTunnelClosed.Error())
	assert.Equal(t, "tunnel has expired", ErrTunnelExpired.Error())
	assert.Equal(t, "invalid tunnel type", ErrInvalidTunnelType.Error())
	assert.Equal(t, "invalid direction for tunnel type", ErrInvalidDirection.Error())
	assert.Equal(t, "runner not connected", ErrRunnerNotConnected.Error())
	assert.Equal(t, "invalid tunnel token", ErrInvalidToken.Error())
	assert.Equal(t, "port is blocked for security", ErrPortBlocked.Error())
	assert.Equal(t, "request blocked due to SSRF protection", ErrSSRFBlocked.Error())
	assert.Equal(t, "failed to connect to local service", ErrConnectionFailed.Error())
}

func TestListOptions_Defaults(t *testing.T) {
	opts := ListOptions{}
	assert.Empty(t, opts.SessionID)
	assert.Empty(t, opts.RunnerID)
	assert.Nil(t, opts.Types)
	assert.False(t, opts.IncludeClosed)
}
