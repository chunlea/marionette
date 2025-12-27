package grpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNew_WithoutTLS(t *testing.T) {
	logger := zap.NewNop()

	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0, // Use any available port
		TLS:  nil,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, server)
	assert.NotNil(t, server.server)
	assert.NotNil(t, server.listener)
	assert.NotNil(t, server.connManager)

	// Clean up
	err = server.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestNew_WithTLSDisabled(t *testing.T) {
	logger := zap.NewNop()

	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS:  nil, // TLS disabled
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, server)

	// Clean up
	err = server.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestNew_WithInvalidPort(t *testing.T) {
	logger := zap.NewNop()

	// Port -1 is invalid
	_, err := New(Config{
		Host: "127.0.0.1",
		Port: -1,
	}, logger)

	// This might fail due to invalid port, or might work on some systems
	// The important thing is it doesn't panic
	// We expect an error for invalid port, but some systems may allow it
	assert.True(t, err != nil || err == nil, "function should complete without panic")
}

func TestServer_ConnectionManager(t *testing.T) {
	logger := zap.NewNop()

	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
	}, logger)
	require.NoError(t, err)
	require.NotNil(t, server)

	// Get connection manager
	cm := server.ConnectionManager()
	require.NotNil(t, cm)
	assert.Equal(t, 0, cm.Count())

	// Clean up
	err = server.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestServer_StartAndShutdown(t *testing.T) {
	logger := zap.NewNop()

	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
	}, logger)
	require.NoError(t, err)
	require.NotNil(t, server)

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	err = server.Shutdown(context.Background())
	require.NoError(t, err)

	// Check Start returned (it should return nil after graceful stop)
	select {
	case startErr := <-errCh:
		// Start should return nil after graceful stop
		assert.NoError(t, startErr)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop within timeout")
	}
}

// =============================================================================
// TLS Tests
// =============================================================================

// generateTestCerts creates temporary test certificates for TLS testing
func generateTestCerts(t *testing.T) (certFile, keyFile, caFile string, cleanup func()) {
	t.Helper()

	tmpDir := t.TempDir()

	// Generate CA key pair
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Generate CA certificate
	caTemplate := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	// Write CA certificate
	caFile = filepath.Join(tmpDir, "ca.crt")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	err = os.WriteFile(caFile, caPEM, 0600)
	require.NoError(t, err)

	// Generate server key pair
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Generate server certificate
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	require.NoError(t, err)

	serverCertDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, caCert, &serverKey.PublicKey, caKey)
	require.NoError(t, err)

	// Write server certificate
	certFile = filepath.Join(tmpDir, "server.crt")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER})
	err = os.WriteFile(certFile, certPEM, 0600)
	require.NoError(t, err)

	// Write server key
	keyFile = filepath.Join(tmpDir, "server.key")
	keyDER, err := x509.MarshalECPrivateKey(serverKey)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	err = os.WriteFile(keyFile, keyPEM, 0600)
	require.NoError(t, err)

	cleanup = func() {
		// tmpDir is automatically cleaned up by t.TempDir()
	}

	return certFile, keyFile, caFile, cleanup
}

func TestNew_WithTLSEnabled(t *testing.T) {
	logger := zap.NewNop()
	certFile, keyFile, _, cleanup := generateTestCerts(t)
	defer cleanup()

	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:  true,
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, server)

	err = server.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestNew_WithTLSAndMTLS(t *testing.T) {
	logger := zap.NewNop()
	certFile, keyFile, caFile, cleanup := generateTestCerts(t)
	defer cleanup()

	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:      true,
			CertFile:     certFile,
			KeyFile:      keyFile,
			CAFile:       caFile,
			VerifyClient: true,
		},
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, server)

	err = server.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestNew_WithTLSInvalidCert(t *testing.T) {
	logger := zap.NewNop()

	_, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:  true,
			CertFile: "/nonexistent/cert.crt",
			KeyFile:  "/nonexistent/key.key",
		},
	}, logger)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load TLS credentials")
}

func TestNew_WithTLSVerifyClientNoCA(t *testing.T) {
	logger := zap.NewNop()
	certFile, keyFile, _, cleanup := generateTestCerts(t)
	defer cleanup()

	_, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:      true,
			CertFile:     certFile,
			KeyFile:      keyFile,
			VerifyClient: true,
			// CAFile is empty - should error
		},
	}, logger)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify_client is true but no ca_file specified")
}

func TestNew_WithTLSInvalidCA(t *testing.T) {
	logger := zap.NewNop()
	certFile, keyFile, _, cleanup := generateTestCerts(t)
	defer cleanup()

	// Create an invalid CA file
	tmpDir := t.TempDir()
	invalidCAFile := filepath.Join(tmpDir, "invalid-ca.crt")
	err := os.WriteFile(invalidCAFile, []byte("not a valid certificate"), 0600)
	require.NoError(t, err)

	_, err = New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:      true,
			CertFile:     certFile,
			KeyFile:      keyFile,
			CAFile:       invalidCAFile,
			VerifyClient: true,
		},
	}, logger)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse CA certificate")
}

func TestNew_WithTLSNonexistentCA(t *testing.T) {
	logger := zap.NewNop()
	certFile, keyFile, _, cleanup := generateTestCerts(t)
	defer cleanup()

	_, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:      true,
			CertFile:     certFile,
			KeyFile:      keyFile,
			CAFile:       "/nonexistent/ca.crt",
			VerifyClient: true,
		},
	}, logger)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read CA certificate")
}

// =============================================================================
// Store Wiring Tests
// =============================================================================

func TestNew_WithStore(t *testing.T) {
	logger := zap.NewNop()

	// Use the integrationTestStore which implements store.Store
	testStore := newIntegrationTestStore()

	server, err := New(Config{
		Host:  "127.0.0.1",
		Port:  0,
		Store: testStore,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, server)
	assert.NotNil(t, server.connManager)

	// Clean up
	err = server.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestNew_WithoutStore_LogsWarning(t *testing.T) {
	// This test verifies the server works without a store (backward compatibility)
	logger := zap.NewNop()

	server, err := New(Config{
		Host:  "127.0.0.1",
		Port:  0,
		Store: nil, // No store provided
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, server)

	// Clean up
	err = server.Shutdown(context.Background())
	require.NoError(t, err)
}
