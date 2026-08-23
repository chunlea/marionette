package network

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProxyConfig(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantHost string
		wantPort int
		wantErr  string
	}{
		{name: "http with port", url: "http://proxy.internal:3128", wantHost: "proxy.internal", wantPort: 3128},
		{name: "http without port", url: "http://proxy.internal", wantHost: "proxy.internal", wantPort: defaultHTTPProxyPort},
		{name: "https without port", url: "https://proxy.internal", wantHost: "proxy.internal", wantPort: defaultHTTPSProxyPort},
		{name: "ip literal", url: "http://10.0.0.9:8080", wantHost: "10.0.0.9", wantPort: 8080},
		{name: "empty", url: "", wantErr: "proxy url is required"},
		{name: "no scheme", url: "proxy.internal:3128", wantErr: "unsupported proxy scheme"},
		{name: "socks", url: "socks5://proxy.internal:1080", wantErr: "unsupported proxy scheme"},
		{name: "no host", url: "http://", wantErr: "has no host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseProxyConfig(tt.url, nil, "")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			ep, err := cfg.Endpoint()
			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, ep.Host)
			assert.Equal(t, tt.wantPort, ep.Port)
		})
	}
}

func TestProxyConfig_NilSafety(t *testing.T) {
	var cfg *ProxyConfig
	assert.ErrorContains(t, cfg.Validate(), "proxy config is nil")
	_, err := cfg.Endpoint()
	assert.ErrorContains(t, err, "proxy config is nil")
	assert.Nil(t, cfg.Env())
}

func TestProxyConfig_Env(t *testing.T) {
	cfg, err := ParseProxyConfig("http://proxy.internal:3128", []string{"registry.local"}, "")
	require.NoError(t, err)

	env := cfg.Env("marionette.internal:9090")

	assert.Equal(t, "http://proxy.internal:3128", env["HTTP_PROXY"])
	assert.Equal(t, "http://proxy.internal:3128", env["HTTPS_PROXY"])
	assert.Equal(t, "http://proxy.internal:3128", env["http_proxy"])
	assert.Equal(t, "http://proxy.internal:3128", env["https_proxy"])
	assert.Equal(t, env["NO_PROXY"], env["no_proxy"])

	// Loopback always bypasses; the control-plane host is added without its port
	// so the agent's gRPC connection is never proxied.
	noProxy := strings.Split(env["NO_PROXY"], ",")
	assert.Equal(t, []string{"localhost", "127.0.0.1", "::1"}, noProxy[:3])
	assert.Contains(t, noProxy, "marionette.internal")
	assert.NotContains(t, env["NO_PROXY"], "9090")
	assert.Contains(t, noProxy, "registry.local")

	// No CA configured: no certificate variables.
	assert.NotContains(t, env, "SSL_CERT_FILE")
	assert.NotContains(t, env, "NODE_EXTRA_CA_CERTS")
}

func TestProxyConfig_EnvWithCACert(t *testing.T) {
	cfg, err := ParseProxyConfig("http://proxy.internal:3128", nil, "/etc/marionette/proxy-ca.crt")
	require.NoError(t, err)

	env := cfg.Env()

	for _, key := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO"} {
		assert.Equal(t, "/etc/marionette/proxy-ca.crt", env[key], key)
	}
}

func TestProxyConfig_NoProxyIsDeterministicAndDeduped(t *testing.T) {
	cfg, err := ParseProxyConfig("http://p:1", []string{"b.example", "a.example", "b.example"}, "")
	require.NoError(t, err)

	first := cfg.Env("a.example", "localhost")["NO_PROXY"]
	second := cfg.Env("localhost", "a.example")["NO_PROXY"]

	assert.Equal(t, first, second, "NO_PROXY must not churn between spawns")
	assert.Equal(t, "localhost,127.0.0.1,::1,a.example,b.example", first)
}
