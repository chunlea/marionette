package grpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// assertHandshakeRejected asserts that an RPC never reached the service because
// the server refused the connection.
//
// A rejected handshake reaches the client in more than one shape. Sometimes it
// is the TLS error itself ("unknown authority", "certificate required");
// sometimes the server closes the socket first and the client's write loses the
// race, so the same rejection arrives as a broken pipe, a reset or an EOF.
// Asserting on one particular string made these tests flake - which is exactly
// how TestMTLS_RejectedWithWrongCA failed under Docker while passing on macOS.
//
// What actually matters is the gRPC status: a refused connection is a transport
// failure (Unavailable), never an application-level response. Callers pair this
// with a positive control so an Unavailable caused by a server that simply is
// not listening cannot pass for a rejection.
func assertHandshakeRejected(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err),
		"a refused connection must surface as Unavailable, got: %v", err)
}

// assertHandshakeAccepted is the positive control: the same server, reached
// with valid credentials, lets the call through to the service. The RPC still
// fails (the token is invalid) but it fails with an application-level status,
// which proves the transport was established.
func assertHandshakeAccepted(t *testing.T, addr string, certs mtlsCerts) {
	t.Helper()

	creds, err := loadClientCredentials(certs.clientCert, certs.clientKey, certs.caCert)
	require.NoError(t, err)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = pb.NewRunnerServiceClient(conn).RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Name:     "control-runner",
		Hostname: "control-host",
		Token:    "invalid-token",
	})
	require.Error(t, err)
	assert.NotEqual(t, codes.Unavailable, status.Code(err),
		"positive control: a valid client must reach the service, got: %v", err)
}

// TestMTLS_SuccessfulConnection tests that a client with a valid certificate
// can successfully connect to an mTLS-enabled server.
func TestMTLS_SuccessfulConnection(t *testing.T) {
	logger := zap.NewNop()
	certs := generateMTLSCerts(t)

	// Start server with mTLS
	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:      true,
			CertFile:     certs.serverCert,
			KeyFile:      certs.serverKey,
			CAFile:       certs.caCert,
			VerifyClient: true,
		},
	}, logger)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()
	defer func() {
		_ = server.Shutdown(context.Background())
		<-errCh
	}()

	// Give server time to start (longer for CI environments)
	time.Sleep(500 * time.Millisecond)

	// Create client with valid certificate
	clientCreds, err := loadClientCredentials(certs.clientCert, certs.clientKey, certs.caCert)
	require.NoError(t, err)

	conn, err := grpc.NewClient(
		server.listener.Addr().String(),
		grpc.WithTransportCredentials(clientCreds),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Make a request to verify connection works
	client := pb.NewRunnerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The registration will fail because we don't have a valid token,
	// but the TLS handshake should succeed
	_, err = client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Name:     "test-runner",
		Hostname: "test-host",
		Token:    "invalid-token",
	})
	// We expect an error, but NOT a TLS handshake error
	require.Error(t, err)
	// If mTLS failed, we would see "connection refused" or "certificate" errors
	assert.NotContains(t, err.Error(), "certificate")
	assert.NotContains(t, err.Error(), "transport")
}

// TestMTLS_RejectedWithoutClientCert tests that a client without a certificate
// is rejected when connecting to an mTLS-enabled server.
func TestMTLS_RejectedWithoutClientCert(t *testing.T) {
	logger := zap.NewNop()
	certs := generateMTLSCerts(t)

	// Start server with mTLS
	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:      true,
			CertFile:     certs.serverCert,
			KeyFile:      certs.serverKey,
			CAFile:       certs.caCert,
			VerifyClient: true,
		},
	}, logger)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()
	defer func() {
		_ = server.Shutdown(context.Background())
		<-errCh
	}()

	time.Sleep(500 * time.Millisecond)

	// Create client WITHOUT a certificate (only CA for server verification)
	caCert, err := os.ReadFile(certs.caCert)
	require.NoError(t, err)

	certPool := x509.NewCertPool()
	require.True(t, certPool.AppendCertsFromPEM(caCert))

	tlsConfig := &tls.Config{
		RootCAs:    certPool,
		MinVersion: tls.VersionTLS12,
		// No client certificate!
	}

	conn, err := grpc.NewClient(
		server.listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Try to make a request - should fail during TLS handshake
	client := pb.NewRunnerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Name:     "test-runner",
		Hostname: "test-host",
		Token:    "test-token",
	})
	require.Error(t, err)
	// The error should indicate certificate/TLS failure
	// Error message varies by platform: "certificate required", "broken pipe", etc.
	assertHandshakeRejected(t, err)
	assertHandshakeAccepted(t, server.listener.Addr().String(), certs)
}

// TestMTLS_RejectedWithWrongCA tests that a client with a certificate signed
// by a different CA is rejected.
func TestMTLS_RejectedWithWrongCA(t *testing.T) {
	logger := zap.NewNop()
	certs := generateMTLSCerts(t)
	wrongCerts := generateMTLSCerts(t) // Different CA

	// Start server with mTLS (using first CA)
	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:      true,
			CertFile:     certs.serverCert,
			KeyFile:      certs.serverKey,
			CAFile:       certs.caCert,
			VerifyClient: true,
		},
	}, logger)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()
	defer func() {
		_ = server.Shutdown(context.Background())
		<-errCh
	}()

	time.Sleep(500 * time.Millisecond)

	// Create client with certificate from WRONG CA
	// Use the wrong CA's client cert but try to verify server with correct CA
	clientCert, err := tls.LoadX509KeyPair(wrongCerts.clientCert, wrongCerts.clientKey)
	require.NoError(t, err)

	caCert, err := os.ReadFile(certs.caCert) // Correct CA for server verification
	require.NoError(t, err)

	certPool := x509.NewCertPool()
	require.True(t, certPool.AppendCertsFromPEM(caCert))

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS12,
	}

	conn, err := grpc.NewClient(
		server.listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Try to make a request - should fail during TLS handshake
	client := pb.NewRunnerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Name:     "test-runner",
		Hostname: "test-host",
		Token:    "test-token",
	})
	assertHandshakeRejected(t, err)

	// Positive control: the same server accepts a client signed by the CA it
	// trusts, so the rejection above was about the certificate and not about
	// the server being unreachable.
	assertHandshakeAccepted(t, server.listener.Addr().String(), certs)
}

// TestMTLS_RejectedWithExpiredCert tests that a client with an expired
// certificate is rejected.
func TestMTLS_RejectedWithExpiredCert(t *testing.T) {
	logger := zap.NewNop()

	tmpDir := t.TempDir()

	// Generate CA
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	caCert, err := x509.ParseCertificate(caCertDER)
	require.NoError(t, err)

	caCertFile := filepath.Join(tmpDir, "ca.crt")
	require.NoError(t, os.WriteFile(caCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER}), 0600))

	// Generate server certificate
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	serverCertDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, caCert, &serverKey.PublicKey, caKey)
	require.NoError(t, err)

	serverCertFile := filepath.Join(tmpDir, "server.crt")
	serverKeyFile := filepath.Join(tmpDir, "server.key")
	require.NoError(t, os.WriteFile(serverCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER}), 0600))

	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(serverKeyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER}), 0600))

	// Generate EXPIRED client certificate
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Certificate expired 1 hour ago
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     time.Now().Add(-1 * time.Hour), // EXPIRED
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	clientCertDER, err := x509.CreateCertificate(rand.Reader, &clientTemplate, caCert, &clientKey.PublicKey, caKey)
	require.NoError(t, err)

	clientCertFile := filepath.Join(tmpDir, "client.crt")
	clientKeyFile := filepath.Join(tmpDir, "client.key")
	require.NoError(t, os.WriteFile(clientCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER}), 0600))

	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(clientKeyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyDER}), 0600))

	// Start server with mTLS
	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:      true,
			CertFile:     serverCertFile,
			KeyFile:      serverKeyFile,
			CAFile:       caCertFile,
			VerifyClient: true,
		},
	}, logger)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()
	defer func() {
		_ = server.Shutdown(context.Background())
		<-errCh
	}()

	time.Sleep(500 * time.Millisecond)

	// Create client with expired certificate
	clientCreds, err := loadClientCredentials(clientCertFile, clientKeyFile, caCertFile)
	require.NoError(t, err)

	conn, err := grpc.NewClient(
		server.listener.Addr().String(),
		grpc.WithTransportCredentials(clientCreds),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Try to make a request - should fail due to expired cert
	client := pb.NewRunnerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Name:     "test-runner",
		Hostname: "test-host",
		Token:    "test-token",
	})
	require.Error(t, err)
	// The error should indicate certificate is expired or TLS failure
	// Error messages vary significantly across platforms and Go versions
	assertHandshakeRejected(t, err)
}

// TestTLS_ServerOnlyNoClientVerification tests TLS without mTLS
// (server presents cert, client verifies, but server doesn't require client cert).
func TestTLS_ServerOnlyNoClientVerification(t *testing.T) {
	logger := zap.NewNop()
	certs := generateMTLSCerts(t)

	// Start server with TLS but WITHOUT client verification
	server, err := New(Config{
		Host: "127.0.0.1",
		Port: 0,
		TLS: &config.TLSConfig{
			Enabled:      true,
			CertFile:     certs.serverCert,
			KeyFile:      certs.serverKey,
			VerifyClient: false, // No client cert required
		},
	}, logger)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()
	defer func() {
		_ = server.Shutdown(context.Background())
		<-errCh
	}()

	time.Sleep(500 * time.Millisecond)

	// Create client WITHOUT a certificate
	caCert, err := os.ReadFile(certs.caCert)
	require.NoError(t, err)

	certPool := x509.NewCertPool()
	require.True(t, certPool.AppendCertsFromPEM(caCert))

	tlsConfig := &tls.Config{
		RootCAs:    certPool,
		MinVersion: tls.VersionTLS12,
		// No client certificate!
	}

	conn, err := grpc.NewClient(
		server.listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Make a request - TLS handshake should succeed
	client := pb.NewRunnerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Name:     "test-runner",
		Hostname: "test-host",
		Token:    "invalid-token",
	})
	// We expect an application-level error, not TLS error
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "certificate")
	assert.NotContains(t, err.Error(), "tls:")
}

// =============================================================================
// Helper types and functions
// =============================================================================

type mtlsCerts struct {
	caCert     string
	serverCert string
	serverKey  string
	clientCert string
	clientKey  string
}

func generateMTLSCerts(t *testing.T) mtlsCerts {
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
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	caCert, err := x509.ParseCertificate(caCertDER)
	require.NoError(t, err)

	// Write CA certificate
	caCertFile := filepath.Join(tmpDir, "ca.crt")
	require.NoError(t, os.WriteFile(caCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER}), 0600))

	// Generate server key pair
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Generate server certificate
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	serverCertDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, caCert, &serverKey.PublicKey, caKey)
	require.NoError(t, err)

	serverCertFile := filepath.Join(tmpDir, "server.crt")
	require.NoError(t, os.WriteFile(serverCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER}), 0600))

	serverKeyFile := filepath.Join(tmpDir, "server.key")
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(serverKeyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER}), 0600))

	// Generate client key pair
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Generate client certificate
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	clientCertDER, err := x509.CreateCertificate(rand.Reader, &clientTemplate, caCert, &clientKey.PublicKey, caKey)
	require.NoError(t, err)

	clientCertFile := filepath.Join(tmpDir, "client.crt")
	require.NoError(t, os.WriteFile(clientCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER}), 0600))

	clientKeyFile := filepath.Join(tmpDir, "client.key")
	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(clientKeyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyDER}), 0600))

	return mtlsCerts{
		caCert:     caCertFile,
		serverCert: serverCertFile,
		serverKey:  serverKeyFile,
		clientCert: clientCertFile,
		clientKey:  clientKeyFile,
	}
}

func loadClientCredentials(certFile, keyFile, caFile string) (credentials.TransportCredentials, error) {
	clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS12,
	}

	return credentials.NewTLS(tlsConfig), nil
}
