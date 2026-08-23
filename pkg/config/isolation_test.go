package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkIsolationConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     NetworkIsolationConfig
		wantErr string
	}{
		{name: "empty is valid"},
		{
			name: "full configuration",
			cfg: NetworkIsolationConfig{
				ServerURL:       "marionette.internal:9090",
				ProxyURL:        "http://proxy.internal:3128",
				ProxyNoProxy:    []string{"registry.local"},
				ProxyCACert:     "/etc/marionette/proxy-ca.crt",
				DNSServers:      []string{"10.0.0.53", "2001:db8::53"},
				DNSNamespace:    "kube-system",
				RefreshInterval: "2m",
			},
		},
		{
			name:    "malformed server address",
			cfg:     NetworkIsolationConfig{ServerURL: "://nonsense"},
			wantErr: "server_url",
		},
		{
			name:    "proxy without a scheme",
			cfg:     NetworkIsolationConfig{ProxyURL: "proxy.internal:3128"},
			wantErr: "proxy_url",
		},
		{
			name:    "resolver that is not an address",
			cfg:     NetworkIsolationConfig{DNSServers: []string{"dns.example.com"}},
			wantErr: "dns_servers",
		},
		{
			name:    "unparseable refresh interval",
			cfg:     NetworkIsolationConfig{RefreshInterval: "soon"},
			wantErr: "refresh_interval",
		},
		{
			name:    "negative refresh interval",
			cfg:     NetworkIsolationConfig{RefreshInterval: "-1m"},
			wantErr: "must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}

	var nilCfg *NetworkIsolationConfig
	assert.NoError(t, nilCfg.Validate())
}

func TestNetworkIsolationConfig_ProviderJSON(t *testing.T) {
	var nilCfg *NetworkIsolationConfig
	assert.Nil(t, nilCfg.ProviderJSON())

	// Nothing configured means nothing to send: an empty isolation block in a
	// provider's JSON would be indistinguishable from a deliberate one.
	assert.Nil(t, (&NetworkIsolationConfig{}).ProviderJSON())

	cfg := &NetworkIsolationConfig{
		ServerURL:       "marionette.internal:9090",
		ProxyURL:        "http://proxy.internal:3128",
		ProxyNoProxy:    []string{"registry.local"},
		ProxyCACert:     "/ca.crt",
		DNSServers:      []string{"10.0.0.53"},
		DNSNamespace:    "kube-system",
		RefreshInterval: "2m",
	}

	// The keys are the provider's JSON field names, not the YAML ones.
	assert.Equal(t, map[string]interface{}{
		"server_url":       "marionette.internal:9090",
		"proxy_url":        "http://proxy.internal:3128",
		"proxy_no_proxy":   []string{"registry.local"},
		"proxy_ca_cert":    "/ca.crt",
		"dns_servers":      []string{"10.0.0.53"},
		"dns_namespace":    "kube-system",
		"refresh_interval": "2m",
	}, cfg.ProviderJSON())
}

func TestConfig_ValidateRejectsBadProviderIsolation(t *testing.T) {
	cfg := validTestConfig()
	cfg.Providers.Docker = &DockerProviderConfig{
		Isolation: NetworkIsolationConfig{ProxyURL: "ftp://proxy.internal"},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "providers.docker.isolation")

	cfg = validTestConfig()
	cfg.Providers.Kubernetes = &KubernetesProviderConfig{
		Isolation: NetworkIsolationConfig{ServerURL: "://nonsense"},
	}

	err = cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "providers.kubernetes.isolation")
}

// validTestConfig returns a config that passes validation, so a test can make
// exactly one thing invalid.
func validTestConfig() *Config {
	return &Config{
		Server: ServerConfig{
			API:   EndpointConfig{Host: "0.0.0.0", Port: 8080},
			Admin: EndpointConfig{Host: "127.0.0.1", Port: 8081},
			GRPC:  EndpointConfig{Host: "0.0.0.0", Port: 9090},
		},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Storage: StorageConfig{
			Provider: "local",
			Local:    &LocalStorageConfig{Path: "./data"},
		},
	}
}
