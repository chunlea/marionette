// Package certreloader provides automatic certificate reloading for TLS connections.
// It watches certificate files for changes and reloads them automatically,
// enabling zero-downtime certificate rotation.
package certreloader

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// CertReloader manages automatic certificate reloading for TLS connections.
// It watches certificate and key files for changes and reloads them atomically.
// If a reload fails, the old certificate is retained to prevent service disruption.
type CertReloader struct {
	certFile string
	keyFile  string
	cert     atomic.Pointer[tls.Certificate]
	logger   *zap.Logger

	watcher *fsnotify.Watcher
	mu      sync.Mutex
	closed  bool

	// debounceDelay is the time to wait after a file change before reloading.
	// This prevents multiple reloads when both cert and key are updated.
	debounceDelay time.Duration

	// Callbacks for testing and monitoring
	onReload func(err error)
}

// Option configures a CertReloader.
type Option func(*CertReloader)

// WithDebounceDelay sets the debounce delay for file change events.
// Default is 100ms.
func WithDebounceDelay(d time.Duration) Option {
	return func(r *CertReloader) {
		r.debounceDelay = d
	}
}

// WithOnReload sets a callback that is invoked after each reload attempt.
// The callback receives nil on success or the error on failure.
func WithOnReload(fn func(err error)) Option {
	return func(r *CertReloader) {
		r.onReload = fn
	}
}

// New creates a new CertReloader for the given certificate and key files.
// It loads the initial certificate and sets up file watching.
func New(certFile, keyFile string, logger *zap.Logger, opts ...Option) (*CertReloader, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("certificate and key file paths are required")
	}

	r := &CertReloader{
		certFile:      certFile,
		keyFile:       keyFile,
		logger:        logger.Named("certreloader"),
		debounceDelay: 100 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(r)
	}

	// Load initial certificate
	if err := r.reload(); err != nil {
		return nil, fmt.Errorf("loading initial certificate: %w", err)
	}

	// Create file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating file watcher: %w", err)
	}
	r.watcher = watcher

	// Watch the directory containing the files (fsnotify requires directory watch for some editors)
	certDir := filepath.Dir(certFile)
	keyDir := filepath.Dir(keyFile)

	if err := watcher.Add(certDir); err != nil {
		_ = watcher.Close()
		return nil, fmt.Errorf("watching certificate directory %s: %w", certDir, err)
	}

	if certDir != keyDir {
		if err := watcher.Add(keyDir); err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("watching key directory %s: %w", keyDir, err)
		}
	}

	r.logger.Info("certificate reloader initialized",
		zap.String("cert_file", certFile),
		zap.String("key_file", keyFile),
	)

	return r, nil
}

// reload loads the certificate from disk and stores it atomically.
func (r *CertReloader) reload() error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("loading certificate: %w", err)
	}

	r.cert.Store(&cert)
	r.logger.Info("certificate loaded",
		zap.String("cert_file", r.certFile),
		zap.Int("cert_count", len(cert.Certificate)),
	)

	return nil
}

// GetCertificate returns the current certificate for server TLS configuration.
// This implements the tls.Config.GetCertificate callback.
func (r *CertReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := r.cert.Load()
	if cert == nil {
		return nil, fmt.Errorf("no certificate loaded")
	}
	return cert, nil
}

// GetClientCertificate returns the current certificate for client TLS configuration.
// This implements the tls.Config.GetClientCertificate callback.
func (r *CertReloader) GetClientCertificate(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	cert := r.cert.Load()
	if cert == nil {
		return nil, fmt.Errorf("no certificate loaded")
	}
	return cert, nil
}

// Certificate returns the current certificate directly.
func (r *CertReloader) Certificate() *tls.Certificate {
	return r.cert.Load()
}

// Watch starts watching for file changes and reloads certificates as needed.
// It blocks until the context is canceled or an unrecoverable error occurs.
// File change errors are logged but do not cause Watch to return.
func (r *CertReloader) Watch(ctx context.Context) error {
	certBase := filepath.Base(r.certFile)
	keyBase := filepath.Base(r.keyFile)

	// Debounce timer for batching multiple file events
	var debounceTimer *time.Timer
	var debounceC <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("certificate watcher stopped")
			return ctx.Err()

		case event, ok := <-r.watcher.Events:
			if !ok {
				return fmt.Errorf("watcher events channel closed")
			}

			// Check if this is a relevant file
			eventBase := filepath.Base(event.Name)
			if eventBase != certBase && eventBase != keyBase {
				continue
			}

			// Only react to write and create events
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}

			r.logger.Debug("file change detected",
				zap.String("file", event.Name),
				zap.String("op", event.Op.String()),
			)

			// Start or reset debounce timer
			if debounceTimer == nil {
				debounceTimer = time.NewTimer(r.debounceDelay)
				debounceC = debounceTimer.C
			} else {
				if !debounceTimer.Stop() {
					select {
					case <-debounceTimer.C:
					default:
					}
				}
				debounceTimer.Reset(r.debounceDelay)
			}

		case <-debounceC:
			debounceTimer = nil
			debounceC = nil

			r.logger.Info("reloading certificate after file change")
			if err := r.reload(); err != nil {
				r.logger.Error("failed to reload certificate, keeping old certificate",
					zap.Error(err),
				)
				if r.onReload != nil {
					r.onReload(err)
				}
			} else {
				r.logger.Info("certificate reloaded successfully")
				if r.onReload != nil {
					r.onReload(nil)
				}
			}

		case err, ok := <-r.watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher errors channel closed")
			}
			r.logger.Error("file watcher error", zap.Error(err))
		}
	}
}

// Close stops the certificate reloader and releases resources.
func (r *CertReloader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	if r.watcher != nil {
		if err := r.watcher.Close(); err != nil {
			return fmt.Errorf("closing file watcher: %w", err)
		}
	}

	r.logger.Info("certificate reloader closed")
	return nil
}

// MustReload forces an immediate reload of the certificate.
// This is useful for testing or when you know the certificate has changed
// but don't want to wait for the file watcher.
func (r *CertReloader) MustReload() error {
	return r.reload()
}

// WatchFiles returns the list of files being watched.
func (r *CertReloader) WatchFiles() []string {
	return []string{r.certFile, r.keyFile}
}

// NewTLSConfig creates a tls.Config that uses the CertReloader for dynamic certificates.
// For server configurations, set clientAuth as needed.
// For client configurations, the RootCAs should be set separately.
func (r *CertReloader) NewTLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate:       r.GetCertificate,
		GetClientCertificate: r.GetClientCertificate,
		MinVersion:           tls.VersionTLS12,
	}
}

// NewServerTLSConfig creates a server tls.Config with optional mTLS support.
// If caFile is provided and verifyClient is true, client certificate verification is enabled.
func NewServerTLSConfig(r *CertReloader, caFile string, verifyClient bool) (*tls.Config, error) {
	tlsConfig := r.NewTLSConfig()

	if caFile != "" && verifyClient {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA certificate: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsConfig, nil
}

// NewClientTLSConfig creates a client tls.Config with certificate verification.
func NewClientTLSConfig(r *CertReloader, caFile string, skipVerify bool) (*tls.Config, error) {
	tlsConfig := r.NewTLSConfig()
	tlsConfig.InsecureSkipVerify = skipVerify

	if caFile != "" && !skipVerify {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA certificate: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.RootCAs = certPool
	}

	return tlsConfig, nil
}
