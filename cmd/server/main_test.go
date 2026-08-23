package main

import (
	"testing"

	"github.com/chunlea/marionette/pkg/config"
	"github.com/stretchr/testify/assert"
)

// TestLocalAPIAddr covers the bind-address-versus-dial-target distinction: the
// admin server proxies /api/v1 to the public API, and the configured host is
// where that API listens, not necessarily something that can be dialled.
func TestLocalAPIAddr(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.EndpointConfig
		want string
	}{
		{"wildcard becomes loopback", config.EndpointConfig{Host: "0.0.0.0", Port: 8080}, "http://127.0.0.1:8080"},
		{"empty becomes loopback", config.EndpointConfig{Port: 8080}, "http://127.0.0.1:8080"},
		{"ipv6 wildcard becomes loopback", config.EndpointConfig{Host: "::", Port: 8080}, "http://127.0.0.1:8080"},
		{"bracketed ipv6 wildcard becomes loopback", config.EndpointConfig{Host: "[::]", Port: 8080}, "http://127.0.0.1:8080"},
		{"loopback is kept", config.EndpointConfig{Host: "127.0.0.1", Port: 9000}, "http://127.0.0.1:9000"},
		// A concrete bind address is used as-is: an API bound to one interface
		// may not be listening on the loopback at all.
		{"concrete host is kept", config.EndpointConfig{Host: "10.0.0.5", Port: 8080}, "http://10.0.0.5:8080"},
		{"ipv6 host is bracketed", config.EndpointConfig{Host: "::1", Port: 8080}, "http://[::1]:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, localAPIAddr(tt.cfg))
		})
	}
}
