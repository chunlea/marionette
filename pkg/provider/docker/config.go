package docker

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chunlea/marionette/pkg/network"
	"github.com/chunlea/marionette/pkg/provider"
)

const (
	// DefaultHost is the default Docker socket path.
	DefaultHost = "unix:///var/run/docker.sock"

	// DefaultImage is the default container image.
	DefaultImage = "marionette/agent:latest"

	// DefaultLabelPrefix is the default prefix for Docker labels.
	DefaultLabelPrefix = "marionette.dev"

	// DefaultMemory is the default memory limit.
	DefaultMemory = "2g"

	// DefaultCPUs is the default CPU limit.
	DefaultCPUs = "2"
)

// Config holds Docker provider settings parsed from provider_configs.config JSON.
type Config struct {
	// Host is the Docker daemon endpoint.
	// Examples: "unix:///var/run/docker.sock", "tcp://localhost:2376"
	Host string `json:"host"`

	// Image is the container image to use for runners.
	Image string `json:"image"`

	// Network is the Docker network to attach containers to.
	Network string `json:"network"`

	// Resources holds default resource limits.
	Resources ResourceConfig `json:"resources"`

	// LabelPrefix is the prefix for Docker labels (default: marionette.dev).
	LabelPrefix string `json:"label_prefix"`

	// Cmd is the default command to run in the container.
	// If empty, the image's default entrypoint/cmd is used.
	Cmd []string `json:"cmd,omitempty"`

	// Isolation holds the network-isolation settings applied to runners whose
	// session asks for a restricted network policy.
	Isolation IsolationConfig `json:"isolation,omitempty"`
}

// IsolationConfig holds operator-controlled network isolation settings.
//
// Everything here is operator input, never session input: a session picks a
// policy level, it does not get to choose its own proxy or resolvers.
type IsolationConfig struct {
	// ServerURL is the control-plane address pinned into every restricted
	// runner's firewall.
	//
	// SpawnOptions.ServerURL is authoritative when the caller sets it; this is
	// the operator fallback for deployments where it does not.
	ServerURL string `json:"server_url,omitempty"`

	// ProxyURL is the egress proxy used by proxy-level sessions,
	// e.g. "http://proxy.internal:3128".
	ProxyURL string `json:"proxy_url,omitempty"`

	// ProxyNoProxy lists extra hosts that bypass the proxy.
	ProxyNoProxy []string `json:"proxy_no_proxy,omitempty"`

	// ProxyCACert is the in-container path to the proxy's CA bundle, for a
	// proxy that terminates TLS. The runner image or a mount must provide it.
	ProxyCACert string `json:"proxy_ca_cert,omitempty"`

	// DNSServers are the resolver addresses restricted runners may reach.
	//
	// Leaving this empty means allow_list and proxy sessions fall back to
	// permitting DNS to any destination, because nothing in the sandbox works
	// without name resolution. Pinning resolvers here is what closes DNS as an
	// exfiltration channel.
	DNSServers []string `json:"dns_servers,omitempty"`

	// RefreshInterval overrides how often pinned allow-list addresses are
	// re-resolved, e.g. "2m". Clamped to [30s, 15m].
	RefreshInterval string `json:"refresh_interval,omitempty"`
}

// RefreshIntervalDuration parses RefreshInterval, returning 0 when unset.
func (c *IsolationConfig) RefreshIntervalDuration() (time.Duration, error) {
	if c.RefreshInterval == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(c.RefreshInterval)
	if err != nil {
		return 0, fmt.Errorf("invalid refresh_interval %q: %w", c.RefreshInterval, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("refresh_interval must be positive, got %q", c.RefreshInterval)
	}
	return d, nil
}

// ResourceConfig holds default resource limits.
type ResourceConfig struct {
	// Memory is the memory limit (e.g., "2g", "2048m", "2147483648").
	Memory string `json:"memory"`

	// CPUs is the CPU limit (e.g., "2", "1.5").
	CPUs string `json:"cpus"`
}

// ParseConfig parses raw JSON into Config with defaults applied.
func ParseConfig(data json.RawMessage) (*Config, error) {
	var cfg Config
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing docker config: %w", err)
		}
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Host == "" {
		c.Host = DefaultHost
	}
	if c.Image == "" {
		c.Image = DefaultImage
	}
	if c.LabelPrefix == "" {
		c.LabelPrefix = DefaultLabelPrefix
	}
	if c.Resources.Memory == "" {
		c.Resources.Memory = DefaultMemory
	}
	if c.Resources.CPUs == "" {
		c.Resources.CPUs = DefaultCPUs
	}
}

func (c *Config) validate() error {
	if c.Image == "" {
		return &provider.ErrInvalidConfig{Field: "image", Reason: "required"}
	}

	// Validate host format
	if !strings.HasPrefix(c.Host, "unix://") && !strings.HasPrefix(c.Host, "tcp://") {
		return &provider.ErrInvalidConfig{
			Field:  "host",
			Reason: "must start with unix:// or tcp://",
		}
	}

	// Validate memory format
	if _, err := ParseMemory(c.Resources.Memory); err != nil {
		return &provider.ErrInvalidConfig{Field: "resources.memory", Reason: err.Error()}
	}

	// Validate CPU format
	if _, err := ParseCPUs(c.Resources.CPUs); err != nil {
		return &provider.ErrInvalidConfig{Field: "resources.cpus", Reason: err.Error()}
	}

	// A malformed proxy or server address must fail at configuration time, not
	// when the first restricted session tries to spawn.
	if c.Isolation.ProxyURL != "" {
		if _, err := network.ParseProxyConfig(c.Isolation.ProxyURL, c.Isolation.ProxyNoProxy, c.Isolation.ProxyCACert); err != nil {
			return &provider.ErrInvalidConfig{Field: "isolation.proxy_url", Reason: err.Error()}
		}
	}

	if c.Isolation.ServerURL != "" {
		if _, err := network.ParseEndpoint(c.Isolation.ServerURL, network.DefaultControlPlanePort); err != nil {
			return &provider.ErrInvalidConfig{Field: "isolation.server_url", Reason: err.Error()}
		}
	}

	for _, addr := range c.Isolation.DNSServers {
		if net.ParseIP(addr) == nil {
			return &provider.ErrInvalidConfig{
				Field:  "isolation.dns_servers",
				Reason: fmt.Sprintf("%q is not an IP address", addr),
			}
		}
	}

	if _, err := c.Isolation.RefreshIntervalDuration(); err != nil {
		return &provider.ErrInvalidConfig{Field: "isolation.refresh_interval", Reason: err.Error()}
	}

	return nil
}

// memoryPattern matches memory strings like "2g", "2048m", "2147483648".
var memoryPattern = regexp.MustCompile(`^(\d+)([gGmMkK])?[bB]?$`)

// ParseMemory converts a memory string to bytes.
// Supports formats: "2g", "2G", "2gb", "2048m", "2048M", "2147483648"
func ParseMemory(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty memory value")
	}

	matches := memoryPattern.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid memory format: %s", s)
	}

	value, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory value: %s", s)
	}

	unit := strings.ToLower(matches[2])
	switch unit {
	case "g":
		return value * 1024 * 1024 * 1024, nil
	case "m":
		return value * 1024 * 1024, nil
	case "k":
		return value * 1024, nil
	case "":
		return value, nil
	default:
		return 0, fmt.Errorf("unknown memory unit: %s", unit)
	}
}

// ParseCPUs converts a CPU string to a float64.
// Supports formats: "2", "1.5", "0.5"
func ParseCPUs(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty CPU value")
	}

	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CPU value: %s", s)
	}

	if value <= 0 {
		return 0, fmt.Errorf("CPU value must be positive: %s", s)
	}

	return value, nil
}

// MemoryMB returns the memory limit in megabytes.
func (c *Config) MemoryMB() (int, error) {
	bytes, err := ParseMemory(c.Resources.Memory)
	if err != nil {
		return 0, err
	}
	return int(bytes / (1024 * 1024)), nil
}

// CPUs returns the CPU limit as a float.
func (c *Config) CPUs() (float64, error) {
	return ParseCPUs(c.Resources.CPUs)
}

// defaultSuspendConfig returns Docker's suspend defaults. Docker can pause a
// container natively, so pause is the default strategy.
func defaultSuspendConfig() provider.SuspendConfig {
	return provider.SuspendConfig{
		Strategy:    provider.SuspendStrategyPause,
		MinDuration: 60 * time.Second,
		MaxDuration: 24 * time.Hour,
		Fallback:    provider.SuspendStrategyTerminatePreserveStorage,
	}
}
