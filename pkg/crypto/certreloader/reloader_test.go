package certreloader

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
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNew_LoadsInitialCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Verify certificate is loaded
	cert := reloader.Certificate()
	require.NotNil(t, cert)
	assert.Len(t, cert.Certificate, 1)
}

func TestNew_FailsWithMissingCert(t *testing.T) {
	logger := zap.NewNop()
	_, err := New("/nonexistent/cert.crt", "/nonexistent/key.key", logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading initial certificate")
}

func TestNew_FailsWithEmptyPaths(t *testing.T) {
	logger := zap.NewNop()
	_, err := New("", "", logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestGetCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Test GetCertificate callback
	cert, err := reloader.GetCertificate(nil)
	require.NoError(t, err)
	require.NotNil(t, cert)
}

func TestGetClientCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Test GetClientCertificate callback
	cert, err := reloader.GetClientCertificate(nil)
	require.NoError(t, err)
	require.NotNil(t, cert)
}

func TestMustReload(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Get initial cert
	cert1 := reloader.Certificate()
	require.NotNil(t, cert1)

	// Generate new certificate
	generateTestCert(t, tmpDir, "test")

	// Force reload
	err = reloader.MustReload()
	require.NoError(t, err)

	// Verify certificate was reloaded
	cert2 := reloader.Certificate()
	require.NotNil(t, cert2)
}

func TestWatch_ReloadsOnFileChange(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	// Track reload events
	var reloadCount int
	var reloadMu sync.Mutex
	reloadCh := make(chan error, 10)

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger,
		WithDebounceDelay(50*time.Millisecond),
		WithOnReload(func(err error) {
			reloadMu.Lock()
			reloadCount++
			reloadMu.Unlock()
			reloadCh <- err
		}),
	)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Start watching
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = reloader.Watch(ctx)
	}()

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Regenerate certificate (triggers file change)
	generateTestCert(t, tmpDir, "test")

	// Wait for reload
	select {
	case err := <-reloadCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for reload")
	}

	reloadMu.Lock()
	count := reloadCount
	reloadMu.Unlock()
	assert.GreaterOrEqual(t, count, 1)
}

func TestWatch_KeepsOldCertOnReloadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	reloadCh := make(chan error, 10)

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger,
		WithDebounceDelay(50*time.Millisecond),
		WithOnReload(func(err error) {
			reloadCh <- err
		}),
	)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Get original certificate
	origCert := reloader.Certificate()
	require.NotNil(t, origCert)

	// Start watching
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = reloader.Watch(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Write invalid certificate (triggers reload failure)
	err = os.WriteFile(certFile, []byte("invalid cert"), 0600)
	require.NoError(t, err)

	// Wait for failed reload
	select {
	case err := <-reloadCh:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for reload error")
	}

	// Verify old certificate is still active
	currentCert := reloader.Certificate()
	require.NotNil(t, currentCert)
	assert.Equal(t, origCert.Certificate, currentCert.Certificate)
}

func TestWatch_StopsOnContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- reloader.Watch(ctx)
	}()

	// Cancel context
	cancel()

	// Watch should return
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Watch to return")
	}
}

func TestNewTLSConfig(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	tlsConfig := reloader.NewTLSConfig()
	require.NotNil(t, tlsConfig)
	assert.NotNil(t, tlsConfig.GetCertificate)
	assert.NotNil(t, tlsConfig.GetClientCertificate)
	assert.Equal(t, uint16(tls.VersionTLS12), tlsConfig.MinVersion)
}

func TestNewServerTLSConfig(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "server")
	caFile := generateCACert(t, tmpDir)

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	tlsConfig, err := NewServerTLSConfig(reloader, caFile, true)
	require.NoError(t, err)
	require.NotNil(t, tlsConfig)
	assert.Equal(t, tls.RequireAndVerifyClientCert, tlsConfig.ClientAuth)
	assert.NotNil(t, tlsConfig.ClientCAs)
}

func TestNewClientTLSConfig(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "client")
	caFile := generateCACert(t, tmpDir)

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	tlsConfig, err := NewClientTLSConfig(reloader, caFile, false)
	require.NoError(t, err)
	require.NotNil(t, tlsConfig)
	assert.NotNil(t, tlsConfig.RootCAs)
	assert.False(t, tlsConfig.InsecureSkipVerify)
}

func TestClose(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)

	// Close should not error
	err = reloader.Close()
	require.NoError(t, err)

	// Double close should not error
	err = reloader.Close()
	require.NoError(t, err)
}

func TestWatchFiles(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	files := reloader.WatchFiles()
	assert.Len(t, files, 2)
	assert.Contains(t, files, certFile)
	assert.Contains(t, files, keyFile)
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Start multiple goroutines accessing the certificate
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cert, err := reloader.GetCertificate(nil)
				assert.NoError(t, err)
				assert.NotNil(t, cert)
			}
		}()
	}

	// Trigger reloads concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			_ = reloader.MustReload()
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()
}

func TestNew_CertAndKeyInDifferentDirectories(t *testing.T) {
	// Create two separate directories
	certDir := t.TempDir()
	keyDir := t.TempDir()

	// Generate cert and key in certDir first
	certFile, keyFileOrig := generateTestCert(t, certDir, "test")

	// Move key to keyDir
	keyFile := filepath.Join(keyDir, "test.key")
	keyData, err := os.ReadFile(keyFileOrig)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(keyFile, keyData, 0600))

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Verify certificate is loaded
	cert := reloader.Certificate()
	require.NotNil(t, cert)

	// Verify both directories are being watched
	files := reloader.WatchFiles()
	assert.Contains(t, files, certFile)
	assert.Contains(t, files, keyFile)
}

func TestNewServerTLSConfig_InvalidCA(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "server")

	// Create invalid CA file
	invalidCAFile := filepath.Join(tmpDir, "invalid-ca.crt")
	err := os.WriteFile(invalidCAFile, []byte("not a valid certificate"), 0600)
	require.NoError(t, err)

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Should fail to parse invalid CA
	_, err = NewServerTLSConfig(reloader, invalidCAFile, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse CA certificate")
}

func TestNewServerTLSConfig_MissingCAFile(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "server")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Should fail with missing CA file
	_, err = NewServerTLSConfig(reloader, "/nonexistent/ca.crt", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading CA certificate")
}

func TestNewClientTLSConfig_InvalidCA(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "client")

	// Create invalid CA file
	invalidCAFile := filepath.Join(tmpDir, "invalid-ca.crt")
	err := os.WriteFile(invalidCAFile, []byte("not a valid certificate"), 0600)
	require.NoError(t, err)

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Should fail to parse invalid CA
	_, err = NewClientTLSConfig(reloader, invalidCAFile, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse CA certificate")
}

func TestNewClientTLSConfig_MissingCAFile(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "client")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// Should fail with missing CA file
	_, err = NewClientTLSConfig(reloader, "/nonexistent/ca.crt", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading CA certificate")
}

func TestNewClientTLSConfig_SkipVerify(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "client")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// With skipVerify=true, CA file should be ignored
	tlsConfig, err := NewClientTLSConfig(reloader, "", true)
	require.NoError(t, err)
	assert.True(t, tlsConfig.InsecureSkipVerify)
	assert.Nil(t, tlsConfig.RootCAs)
}

func TestNewServerTLSConfig_NoClientVerification(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "server")
	caFile := generateCACert(t, tmpDir)

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	// With verifyClient=false, ClientCAs should not be set
	tlsConfig, err := NewServerTLSConfig(reloader, caFile, false)
	require.NoError(t, err)
	assert.Nil(t, tlsConfig.ClientCAs)
	assert.Equal(t, tls.NoClientCert, tlsConfig.ClientAuth)
}

func TestWatch_StopsWhenWatcherClosed(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger)
	require.NoError(t, err)

	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- reloader.Watch(ctx)
	}()

	// Give watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Close the reloader (which closes the watcher)
	err = reloader.Close()
	require.NoError(t, err)

	// Watch should return with an error about closed channel
	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "closed")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Watch to return")
	}
}

func TestWatch_IgnoresIrrelevantFiles(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	reloadCh := make(chan error, 10)

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger,
		WithDebounceDelay(50*time.Millisecond),
		WithOnReload(func(err error) {
			reloadCh <- err
		}),
	)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = reloader.Watch(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create an unrelated file in the same directory - should be ignored
	unrelatedFile := filepath.Join(tmpDir, "unrelated.txt")
	err = os.WriteFile(unrelatedFile, []byte("test"), 0600)
	require.NoError(t, err)

	// Wait a bit and ensure no reload was triggered
	select {
	case <-reloadCh:
		t.Fatal("reload should not have been triggered for unrelated file")
	case <-time.After(300 * time.Millisecond):
		// Expected - no reload
	}
}

func TestWatch_IgnoresDeleteEvents(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tmpDir, "test")

	reloadCh := make(chan error, 10)

	logger := zap.NewNop()
	reloader, err := New(certFile, keyFile, logger,
		WithDebounceDelay(50*time.Millisecond),
		WithOnReload(func(err error) {
			reloadCh <- err
		}),
	)
	require.NoError(t, err)
	defer func() { _ = reloader.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = reloader.Watch(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create and then delete a file with same base name in a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))
	tempFile := filepath.Join(subDir, "test.crt")
	require.NoError(t, os.WriteFile(tempFile, []byte("temp"), 0600))
	require.NoError(t, os.Remove(tempFile))

	// Wait a bit - no reload should happen (delete events are ignored)
	select {
	case <-reloadCh:
		t.Fatal("reload should not have been triggered for delete event")
	case <-time.After(300 * time.Millisecond):
		// Expected - no reload
	}
}

// =============================================================================
// Test helpers
// =============================================================================

func generateTestCert(t *testing.T, dir, name string) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	certFile = filepath.Join(dir, name+".crt")
	err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0600)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	keyFile = filepath.Join(dir, name+".key")
	err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600)
	require.NoError(t, err)

	return certFile, keyFile
}

func generateCACert(t *testing.T, dir string) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	caFile := filepath.Join(dir, "ca.crt")
	err = os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0600)
	require.NoError(t, err)

	return caFile
}
