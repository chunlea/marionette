package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"go.uber.org/zap"
)

// The dashboard is a single-page app served by the admin server on :8081, and
// it calls both APIs with relative paths: /admin/api/v1 for operator actions
// and /api/v1 for everything else. Under `vite dev` a proxy makes both work.
// In a deployed binary the second one 404s, because the public API lives on
// :8080 — so every page of the shipped dashboard was broken.
//
// The fix is to give the browser one origin. Which origin is not arbitrary:
// serving the SPA from the public API server would mean proxying the admin API
// out through the public port, and the admin API mints API keys, registers
// runners and reads every session. Operators firewall :8081 for exactly that
// reason. So the traffic goes the other way — the more protected origin
// forwards the less privileged API — and the public API keeps authenticating
// every request itself, so nothing is trusted merely for arriving through the
// admin port.
//
// This file provides the forwarder. Mounting it is a one-line change in
// cmd/server, which passes it to admin.New via the WithMiddleware option that
// server already exposes.

// NewUpstreamProxy returns middleware that forwards requests under prefix to
// the API server at target, passing everything else through untouched.
//
// WebSocket upgrades are relayed, including the ?token= form the browser log
// and event streams use.
func NewUpstreamProxy(prefix, target string, logger *zap.Logger) (func(http.Handler) http.Handler, error) {
	if prefix == "" || !strings.HasPrefix(prefix, "/") {
		return nil, fmt.Errorf("upstream proxy: prefix must start with '/', got %q", prefix)
	}
	upstream, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("upstream proxy: parse target %q: %w", target, err)
	}
	if upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("upstream proxy: target %q needs a scheme and a host", target)
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
			// SetURL joins paths onto the target's own path; the upstream is a
			// bare origin, so the inbound path is preserved as-is.
			r.Out.Host = r.In.Host
			r.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Warn("upstream proxy request failed",
				zap.String("path", r.URL.Path),
				zap.String("upstream", upstream.String()),
				zap.Error(err),
			)
			WriteError(w, http.StatusBadGateway, "upstream_unavailable",
				"The API server could not be reached")
		},
	}

	// Match the prefix itself and everything under it, but not a path that
	// merely starts with the same characters: /api/v1thing is not ours.
	prefixWithSlash := strings.TrimSuffix(prefix, "/") + "/"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path != strings.TrimSuffix(prefix, "/") && !strings.HasPrefix(path, prefixWithSlash) {
				next.ServeHTTP(w, r)
				return
			}

			if isWebSocketUpgrade(r) {
				// The admin server wraps requests in a 30-second timeout,
				// which is right for an API call and fatal for a log stream.
				// Drop the deadline for relayed upgrades; the copy loops still
				// end when either side closes the connection.
				r = r.WithContext(context.WithoutCancel(r.Context()))
			}

			proxy.ServeHTTP(w, r)
		})
	}, nil
}
