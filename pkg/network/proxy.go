package network

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Default ports assumed when a proxy URL omits one.
const (
	defaultHTTPProxyPort  = 3128
	defaultHTTPSProxyPort = 3129
)

// ProxyConfig describes the egress proxy used by PolicyProxy.
//
// Marionette enforces proxy mode with an *explicit* proxy (environment
// injection) plus a default-drop egress firewall, not with a transparent
// iptables REDIRECT. The reasoning:
//
//   - Every tool in the runner image (curl, git, npm, pip, node, the Claude
//     CLI) already honours HTTP_PROXY/HTTPS_PROXY. Nothing has to be taught a
//     new protocol.
//   - A REDIRECT-based intercept has to terminate TLS to learn the destination
//     host, which means MITM with a trusted CA for *all* traffic. Explicit
//     proxying keeps CONNECT tunnels intact, so certificate pinning still
//     works unless the operator deliberately deploys a MITM bundle
//     (CACertPath).
//   - The firewall is what actually enforces the policy: only the proxy
//     endpoint and the control plane are reachable. A tool that ignores the
//     environment variables fails closed instead of escaping the policy, which
//     is the exact failure mode a REDIRECT is usually reached for.
//
// Residual limitation: non-HTTP protocols (raw TCP, ssh, git://) only work if
// the proxy supports CONNECT for their ports and the client is configured to
// use it. Everything else is dropped.
type ProxyConfig struct {
	// URL is the proxy endpoint, e.g. "http://proxy.internal:3128".
	URL string

	// NoProxy lists additional hosts that bypass the proxy. The control-plane
	// host and loopback are always added.
	NoProxy []string

	// CACertPath is the in-container path to the proxy's CA bundle. It is only
	// meaningful when the proxy terminates TLS. Marionette does not create this
	// file: the runner image or a mount must provide it.
	CACertPath string
}

// ParseProxyConfig builds and validates a ProxyConfig.
func ParseProxyConfig(rawURL string, noProxy []string, caCertPath string) (*ProxyConfig, error) {
	p := &ProxyConfig{URL: rawURL, NoProxy: noProxy, CACertPath: caCertPath}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// Validate checks that the proxy endpoint is usable.
func (p *ProxyConfig) Validate() error {
	if p == nil {
		return fmt.Errorf("proxy config is nil")
	}
	_, err := p.Endpoint()
	return err
}

// Endpoint returns the proxy host and port.
func (p *ProxyConfig) Endpoint() (Endpoint, error) {
	if p == nil {
		return Endpoint{}, fmt.Errorf("proxy config is nil")
	}

	raw := strings.TrimSpace(p.URL)
	if raw == "" {
		return Endpoint{}, fmt.Errorf("proxy url is required")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, fmt.Errorf("invalid proxy url %q: %w", raw, err)
	}

	var fallback int
	switch u.Scheme {
	case "http":
		fallback = defaultHTTPProxyPort
	case "https":
		fallback = defaultHTTPSProxyPort
	default:
		return Endpoint{}, fmt.Errorf("unsupported proxy scheme %q in %q (want http or https)", u.Scheme, raw)
	}

	host := u.Hostname()
	if host == "" {
		return Endpoint{}, fmt.Errorf("proxy url %q has no host", raw)
	}

	port := fallback
	if s := u.Port(); s != "" {
		port, err = strconv.Atoi(s)
		if err != nil || port <= 0 || port > 65535 {
			return Endpoint{}, fmt.Errorf("proxy url %q has an invalid port", raw)
		}
	}

	return Endpoint{Host: host, Port: port}, nil
}

// Env returns the environment variables that point a runner's tooling at the
// proxy. extraNoProxy is merged into NO_PROXY; callers pass the control-plane
// hosts so the agent's gRPC connection is never proxied.
func (p *ProxyConfig) Env(extraNoProxy ...string) map[string]string {
	if p == nil {
		return nil
	}

	env := map[string]string{
		"HTTP_PROXY":  p.URL,
		"HTTPS_PROXY": p.URL,
		"http_proxy":  p.URL,
		"https_proxy": p.URL,
	}

	noProxy := p.noProxyList(extraNoProxy)
	env["NO_PROXY"] = noProxy
	env["no_proxy"] = noProxy

	if p.CACertPath != "" {
		// One variable per ecosystem: Go/OpenSSL, python-requests, Node, git.
		env["SSL_CERT_FILE"] = p.CACertPath
		env["REQUESTS_CA_BUNDLE"] = p.CACertPath
		env["NODE_EXTRA_CA_CERTS"] = p.CACertPath
		env["GIT_SSL_CAINFO"] = p.CACertPath
	}

	return env
}

// noProxyList builds a deterministic, de-duplicated NO_PROXY value.
func (p *ProxyConfig) noProxyList(extra []string) string {
	seen := map[string]bool{}
	var out []string

	add := func(values ...string) {
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}

	// Loopback must never be proxied: the agent talks to its own sandbox
	// helpers over 127.0.0.1.
	add("localhost", "127.0.0.1", "::1")
	add(p.NoProxy...)
	for _, e := range extra {
		// Callers hand us endpoints; NO_PROXY matches on host, not host:port.
		if host, _, err := net.SplitHostPort(e); err == nil {
			add(host)
			continue
		}
		add(e)
	}

	// Stable order so a container's env does not churn between spawns.
	head, tail := out[:3], out[3:]
	sort.Strings(tail)
	return strings.Join(append(head, tail...), ",")
}
