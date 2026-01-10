package tunnel

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// TCPProxyConfig holds configuration for TCP proxying.
type TCPProxyConfig struct {
	// BufferSize is the size of the read buffer.
	BufferSize int
	// IdleTimeout is the timeout for idle connections.
	IdleTimeout time.Duration
	// MaxConnectionDuration is the maximum duration of a connection.
	MaxConnectionDuration time.Duration
}

// DefaultTCPProxyConfig returns default TCP proxy configuration.
func DefaultTCPProxyConfig() TCPProxyConfig {
	return TCPProxyConfig{
		BufferSize:            32 * 1024, // 32KB
		IdleTimeout:           5 * time.Minute,
		MaxConnectionDuration: 1 * time.Hour,
	}
}

// TCPProxy handles TCP connection proxying through tunnels.
type TCPProxy struct {
	config TCPProxyConfig
}

// NewTCPProxy creates a new TCP proxy with the given configuration.
func NewTCPProxy(config TCPProxyConfig) *TCPProxy {
	return &TCPProxy{config: config}
}

// TCPConnectionHandler extends ConnectionHandler with streaming support.
// For TCP, we need continuous bidirectional streaming instead of request-response.
type TCPConnectionHandler interface {
	ConnectionHandler

	// SendTunnelDataStream sends data continuously to the runner.
	// This is called from a goroutine that reads from the client.
	SendTunnelDataStream(ctx context.Context, tunnelID string, reader io.Reader) error

	// ReceiveTunnelDataStream receives data continuously from the runner.
	// This is called from a goroutine that writes to the client.
	ReceiveTunnelDataStream(ctx context.Context, tunnelID string, writer io.Writer) error
}

// ProxyTCPConnection proxies a TCP connection through the tunnel.
// It handles bidirectional data relay between the client and the runner.
func (p *TCPProxy) ProxyTCPConnection(
	ctx context.Context,
	tunnelID string,
	handler ConnectionHandler,
	conn Connection,
) error {
	// Set up deadline for the entire connection
	if p.config.MaxConnectionDuration > 0 {
		deadline := time.Now().Add(p.config.MaxConnectionDuration)
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("failed to set connection deadline: %w", err)
		}
	}

	// Create a context that can be cancelled
	proxyCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Error channel to collect errors from goroutines
	errCh := make(chan error, 2)

	// WaitGroup to wait for both directions to complete
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Runner direction
	go func() {
		defer wg.Done()
		err := p.relayClientToRunner(proxyCtx, tunnelID, handler, conn)
		if err != nil && err != io.EOF {
			errCh <- fmt.Errorf("client to runner relay failed: %w", err)
		}
		// Cancel context to stop the other direction
		cancel()
	}()

	// Runner -> Client direction
	go func() {
		defer wg.Done()
		err := p.relayRunnerToClient(proxyCtx, tunnelID, handler, conn)
		if err != nil && err != io.EOF {
			errCh <- fmt.Errorf("runner to client relay failed: %w", err)
		}
		// Cancel context to stop the other direction
		cancel()
	}()

	// Wait for both directions to complete
	wg.Wait()
	close(errCh)

	// Return first error if any
	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return nil
}

// relayClientToRunner reads from the client and sends to the runner.
func (p *TCPProxy) relayClientToRunner(
	ctx context.Context,
	tunnelID string,
	handler ConnectionHandler,
	conn Connection,
) error {
	buf := make([]byte, p.config.BufferSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Set read deadline for idle timeout
		if p.config.IdleTimeout > 0 {
			if err := conn.SetReadDeadline(time.Now().Add(p.config.IdleTimeout)); err != nil {
				return fmt.Errorf("failed to set read deadline: %w", err)
			}
		}

		// Read from client
		n, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				return io.EOF
			}
			return fmt.Errorf("failed to read from client: %w", err)
		}

		if n > 0 {
			// Send to runner
			if err := handler.SendTunnelData(ctx, tunnelID, buf[:n]); err != nil {
				return fmt.Errorf("failed to send to runner: %w", err)
			}
		}
	}
}

// relayRunnerToClient receives from the runner and writes to the client.
func (p *TCPProxy) relayRunnerToClient(
	ctx context.Context,
	tunnelID string,
	handler ConnectionHandler,
	conn Connection,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Receive from runner
		data, err := handler.ReceiveTunnelData(ctx, tunnelID)
		if err != nil {
			return fmt.Errorf("failed to receive from runner: %w", err)
		}

		if len(data) > 0 {
			// Set write deadline
			if p.config.IdleTimeout > 0 {
				if err := conn.SetWriteDeadline(time.Now().Add(p.config.IdleTimeout)); err != nil {
					return fmt.Errorf("failed to set write deadline: %w", err)
				}
			}

			// Write to client
			_, err := conn.Write(data)
			if err != nil {
				return fmt.Errorf("failed to write to client: %w", err)
			}
		}
	}
}

// handleTCPProxyConnection is the internal implementation for TCP proxying.
func (m *TunnelManager) handleTCPProxyConnection(
	ctx context.Context,
	tunnelID string,
	handler ConnectionHandler,
	conn Connection,
) error {
	proxy := NewTCPProxy(DefaultTCPProxyConfig())
	return proxy.ProxyTCPConnection(ctx, tunnelID, handler, conn)
}
