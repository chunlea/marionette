package network

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Endpoint is a host:port pair that a policy must keep reachable.
type Endpoint struct {
	// Host is a hostname or IP literal.
	Host string

	// Port is the TCP port.
	Port int
}

// String renders the endpoint as host:port.
func (e Endpoint) String() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

// IsIP reports whether Host is already an IP literal and needs no resolution.
func (e Endpoint) IsIP() bool {
	return net.ParseIP(e.Host) != nil
}

// Validate checks that the endpoint is usable in a firewall rule.
func (e Endpoint) Validate() error {
	if strings.TrimSpace(e.Host) == "" {
		return fmt.Errorf("endpoint host is required")
	}
	if e.Port <= 0 || e.Port > 65535 {
		return fmt.Errorf("endpoint %s has an invalid port %d", e.Host, e.Port)
	}
	return nil
}

// ParseEndpoint parses the address forms the server hands to runners:
//
//	host:port                 localhost:9090
//	host                      marionette.internal    (defaultPort applies)
//	scheme://host:port        https://api.example.com, grpc://server:9090
//	[::1]:9090                IPv6 literals
//
// defaultPort is used when the address carries no explicit port. A zero
// defaultPort makes a port mandatory.
func ParseEndpoint(raw string, defaultPort int) (Endpoint, error) {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return Endpoint{}, fmt.Errorf("endpoint is empty")
	}

	// Strip a scheme if present. gRPC targets such as "dns:///host:9090" also
	// land here and reduce to the trailing authority.
	if i := strings.Index(addr, "://"); i >= 0 {
		u, err := url.Parse(addr)
		if err != nil {
			return Endpoint{}, fmt.Errorf("invalid endpoint %q: %w", raw, err)
		}
		if p := schemeDefaultPort(u.Scheme); p != 0 && defaultPort == 0 {
			defaultPort = p
		}
		addr = u.Host
		if addr == "" {
			// "dns:///host:9090" parses with an empty Host and the authority in
			// Path; take the last non-empty path element.
			addr = strings.Trim(u.Path, "/")
		}
		if addr == "" {
			return Endpoint{}, fmt.Errorf("endpoint %q has no host", raw)
		}
	}

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		// No port in the address: fall back to the default.
		if defaultPort == 0 {
			return Endpoint{}, fmt.Errorf("endpoint %q has no port and no default", raw)
		}
		host = strings.Trim(addr, "[]")
		portStr = strconv.Itoa(defaultPort)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Endpoint{}, fmt.Errorf("endpoint %q has an invalid port: %w", raw, err)
	}

	ep := Endpoint{Host: host, Port: port}
	if err := ep.Validate(); err != nil {
		return Endpoint{}, fmt.Errorf("endpoint %q: %w", raw, err)
	}
	return ep, nil
}

// schemeDefaultPort maps the schemes the server may hand out to a port.
func schemeDefaultPort(scheme string) int {
	switch strings.ToLower(scheme) {
	case "http", "ws":
		return 80
	case "https", "wss":
		return 443
	default:
		return 0
	}
}
