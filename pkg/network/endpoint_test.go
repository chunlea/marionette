package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		defaultPort int
		want        Endpoint
		wantErr     string
	}{
		{
			name: "host and port",
			raw:  "localhost:9090",
			want: Endpoint{Host: "localhost", Port: 9090},
		},
		{
			name:        "host without port uses default",
			raw:         "marionette.internal",
			defaultPort: DefaultControlPlanePort,
			want:        Endpoint{Host: "marionette.internal", Port: 9090},
		},
		{
			name:        "explicit port beats default",
			raw:         "marionette.internal:7000",
			defaultPort: DefaultControlPlanePort,
			want:        Endpoint{Host: "marionette.internal", Port: 7000},
		},
		{
			name: "https scheme implies 443",
			raw:  "https://api.example.com",
			want: Endpoint{Host: "api.example.com", Port: 443},
		},
		{
			name: "http scheme implies 80",
			raw:  "http://api.example.com",
			want: Endpoint{Host: "api.example.com", Port: 80},
		},
		{
			name: "scheme with explicit port",
			raw:  "grpc://server.example.com:9090",
			want: Endpoint{Host: "server.example.com", Port: 9090},
		},
		{
			name: "grpc dns target",
			raw:  "dns:///server.example.com:9090",
			want: Endpoint{Host: "server.example.com", Port: 9090},
		},
		{
			name: "ipv4 literal",
			raw:  "10.1.2.3:9090",
			want: Endpoint{Host: "10.1.2.3", Port: 9090},
		},
		{
			name: "ipv6 literal",
			raw:  "[2001:db8::1]:9090",
			want: Endpoint{Host: "2001:db8::1", Port: 9090},
		},
		{
			name:        "bare ipv6 with default port",
			raw:         "[2001:db8::1]",
			defaultPort: 9090,
			want:        Endpoint{Host: "2001:db8::1", Port: 9090},
		},
		{
			name:    "empty",
			raw:     "   ",
			wantErr: "endpoint is empty",
		},
		{
			name:    "no port and no default",
			raw:     "marionette.internal",
			wantErr: "no port and no default",
		},
		{
			name:    "port out of range",
			raw:     "host:70000",
			wantErr: "invalid port",
		},
		{
			name:    "scheme without host",
			raw:     "grpc://",
			wantErr: "has no host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEndpoint(tt.raw, tt.defaultPort)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEndpoint_Validate(t *testing.T) {
	require.NoError(t, Endpoint{Host: "h", Port: 1}.Validate())
	require.NoError(t, Endpoint{Host: "h", Port: 65535}.Validate())

	assert.ErrorContains(t, Endpoint{Port: 80}.Validate(), "host is required")
	assert.ErrorContains(t, Endpoint{Host: "  ", Port: 80}.Validate(), "host is required")
	assert.ErrorContains(t, Endpoint{Host: "h"}.Validate(), "invalid port")
	assert.ErrorContains(t, Endpoint{Host: "h", Port: 65536}.Validate(), "invalid port")
}

func TestEndpoint_StringAndIsIP(t *testing.T) {
	assert.Equal(t, "example.com:9090", Endpoint{Host: "example.com", Port: 9090}.String())
	assert.Equal(t, "[2001:db8::1]:443", Endpoint{Host: "2001:db8::1", Port: 443}.String())

	assert.True(t, Endpoint{Host: "10.0.0.1", Port: 1}.IsIP())
	assert.True(t, Endpoint{Host: "2001:db8::1", Port: 1}.IsIP())
	assert.False(t, Endpoint{Host: "example.com", Port: 1}.IsIP())
}
