package agent

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/streaming/browser"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// BrowserStreamClient manages the gRPC stream for browser content.
type BrowserStreamClient struct {
	logger    *zap.Logger
	client    pb.RunnerServiceClient
	provider  browser.Provider
	tunnelID  string
	runnerID  string
	sessionID string
	token     string

	mu       sync.Mutex
	stream   grpc.BidiStreamingClient[pb.RunnerBrowserMessage, pb.ServerBrowserMessage]
	cancel   context.CancelFunc
	closed   bool
	stopOnce sync.Once
}

// BrowserStreamConfig holds configuration for the browser stream.
type BrowserStreamConfig struct {
	TunnelID      string
	RunnerID      string
	SessionID     string
	Token         string
	LocalPort     int
	Provider      browser.Provider
	Client        pb.RunnerServiceClient
	StreamOptions *browser.StreamOptions
	Logger        *zap.Logger
}

// NewBrowserStreamClient creates a new browser stream client.
func NewBrowserStreamClient(cfg BrowserStreamConfig) (*BrowserStreamClient, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("gRPC client is required")
	}
	if cfg.Provider == nil {
		return nil, fmt.Errorf("browser provider is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	return &BrowserStreamClient{
		logger:    logger.Named("browser-stream"),
		client:    cfg.Client,
		provider:  cfg.Provider,
		tunnelID:  cfg.TunnelID,
		runnerID:  cfg.RunnerID,
		sessionID: cfg.SessionID,
		token:     cfg.Token,
	}, nil
}

// Start starts the browser streaming.
func (c *BrowserStreamClient) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	// Create a cancellable context
	streamCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()

	// Add tunnel token to metadata
	md := metadata.New(map[string]string{
		"x-tunnel-token": c.token,
	})
	streamCtx = metadata.NewOutgoingContext(streamCtx, md)

	// Start the gRPC stream
	stream, err := c.client.StreamBrowser(streamCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to start browser stream: %w", err)
	}

	c.mu.Lock()
	c.stream = stream
	c.mu.Unlock()

	// Send init message
	initMsg := &pb.RunnerBrowserMessage{
		Payload: &pb.RunnerBrowserMessage_Init{
			Init: &pb.BrowserStreamInit{
				TunnelId:  c.tunnelID,
				SessionId: c.sessionID,
				RunnerId:  c.runnerID,
			},
		},
	}
	if err := stream.Send(initMsg); err != nil {
		cancel()
		return fmt.Errorf("failed to send init message: %w", err)
	}

	c.logger.Info("browser stream started",
		zap.String("tunnel_id", c.tunnelID),
		zap.String("session_id", c.sessionID),
	)

	// Start the provider if not already streaming
	if err := c.provider.Start(streamCtx, nil); err != nil {
		cancel()
		return fmt.Errorf("failed to start provider: %w", err)
	}

	// Start goroutines for frame sending and input receiving
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		c.sendFrames(streamCtx, stream)
	}()

	go func() {
		defer wg.Done()
		c.receiveInput(streamCtx, stream)
	}()

	// Wait for context cancellation or completion
	go func() {
		wg.Wait()
		c.cleanup()
	}()

	return nil
}

// sendFrames reads frames from the provider and sends them to the server.
func (c *BrowserStreamClient) sendFrames(ctx context.Context, stream grpc.BidiStreamingClient[pb.RunnerBrowserMessage, pb.ServerBrowserMessage]) {
	frameCh := c.provider.Frames()
	statsTicker := time.NewTicker(5 * time.Second)
	defer statsTicker.Stop()

	var framesSent uint64
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return

		case frame, ok := <-frameCh:
			if !ok {
				c.logger.Debug("frame channel closed")
				return
			}

			// Convert to protobuf
			pbFrame := &pb.BrowserFrame{
				Data:            frame.Data,
				Format:          string(frame.Format),
				Width:           int32(frame.Width),
				Height:          int32(frame.Height),
				Sequence:        frame.Sequence,
				TimestampUnixMs: frame.Timestamp.UnixMilli(),
			}

			// Send frame
			msg := &pb.RunnerBrowserMessage{
				Payload: &pb.RunnerBrowserMessage_Frame{
					Frame: pbFrame,
				},
			}
			if err := stream.Send(msg); err != nil {
				c.logger.Error("failed to send frame", zap.Error(err))
				return
			}
			framesSent++

		case <-statsTicker.C:
			// Send periodic stats
			elapsed := time.Since(startTime).Seconds()
			fps := float64(0)
			if elapsed > 0 {
				fps = float64(framesSent) / elapsed
			}

			stats := c.provider.Stats()
			statsMsg := &pb.RunnerBrowserMessage{
				Payload: &pb.RunnerBrowserMessage_Stats{
					Stats: &pb.BrowserStreamStats{
						FramesSent:      framesSent,
						FramesDropped:   stats.FramesDropped,
						BytesSent:       stats.BytesSent,
						AverageFps:      fps,
						CurrentFps:      fps,
						StartedAtUnixMs: startTime.UnixMilli(),
					},
				},
			}
			if err := stream.Send(statsMsg); err != nil {
				c.logger.Debug("failed to send stats", zap.Error(err))
				// Don't return on stats error, continue streaming
			}
		}
	}
}

// receiveInput receives input events from the server and forwards them to the provider.
func (c *BrowserStreamClient) receiveInput(ctx context.Context, stream grpc.BidiStreamingClient[pb.RunnerBrowserMessage, pb.ServerBrowserMessage]) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				c.logger.Debug("server closed stream")
				return
			}
			if ctx.Err() != nil {
				return
			}
			c.logger.Error("failed to receive message", zap.Error(err))
			return
		}

		// Handle input event
		if input := msg.GetInput(); input != nil {
			c.handleInputEvent(ctx, input)
		}

		// Handle control message
		if ctrl := msg.GetControl(); ctrl != nil {
			c.handleControlMessage(ctx, ctrl)
		}

		// Handle start command (server requesting start)
		if start := msg.GetStart(); start != nil {
			c.logger.Info("received start command")
			// Start command usually means resume/start streaming
			// The options come from the Options field
			if start.GetOptions() != nil {
				opts := start.GetOptions()
				c.logger.Debug("start options",
					zap.Int32("quality", opts.GetQuality()),
					zap.Int32("max_fps", opts.GetMaxFps()),
				)
			}
			// If provider is paused, resume it
			_ = c.provider.Resume(ctx)
		}

		// Handle stop command
		if msg.GetStop() {
			c.logger.Info("received stop command")
			c.Stop()
			return
		}
	}
}

// handleInputEvent processes an input event from the server.
func (c *BrowserStreamClient) handleInputEvent(ctx context.Context, input *pb.BrowserInputEvent) {
	// Convert to internal format
	event := &browser.InputEvent{
		Type:      browser.InputEventType(input.GetType()),
		Timestamp: time.UnixMilli(input.GetTimestampUnixMs()),
	}

	if mouse := input.GetMouse(); mouse != nil {
		event.Mouse = &browser.MouseEvent{
			X:      mouse.GetX(),
			Y:      mouse.GetY(),
			Button: browser.MouseButton(mouse.GetButton()),
			DeltaX: mouse.GetDeltaX(),
			DeltaY: mouse.GetDeltaY(),
		}
	}

	if kb := input.GetKeyboard(); kb != nil {
		event.Keyboard = &browser.KeyboardEvent{
			Key:  kb.GetKey(),
			Code: kb.GetCode(),
			Text: kb.GetText(),
		}
	}

	if err := c.provider.SendInput(ctx, event); err != nil {
		c.logger.Debug("failed to send input to provider",
			zap.String("type", string(event.Type)),
			zap.Error(err),
		)
	}
}

// handleControlMessage processes a control message from the server.
func (c *BrowserStreamClient) handleControlMessage(ctx context.Context, ctrl *pb.BrowserStreamControl) {
	if ctrl.GetPause() {
		c.logger.Debug("received pause command")
		_ = c.provider.Pause(ctx)
		return
	}

	if nav := ctrl.GetNavigate(); nav != nil {
		c.logger.Info("received navigate command", zap.String("url", nav.GetUrl()))
		_ = c.provider.Navigate(ctx, &browser.NavigateRequest{
			URL:      nav.GetUrl(),
			Referrer: nav.GetReferrer(),
		})
		return
	}

	if ctrl.GetSwitchTab() != "" {
		c.logger.Info("received switch tab command", zap.String("tab_id", ctrl.GetSwitchTab()))
		_ = c.provider.SwitchTab(ctx, ctrl.GetSwitchTab())
		return
	}

	if opts := ctrl.GetUpdateOptions(); opts != nil {
		c.logger.Debug("received update options command")
		// Options updates would need to restart the stream with new settings
		// For now just log it - full implementation would stop/restart with new opts
	}
}

// Stop stops the browser streaming.
func (c *BrowserStreamClient) Stop() {
	c.stopOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		if c.cancel != nil {
			c.cancel()
		}
		c.mu.Unlock()

		c.cleanup()
	})
}

// cleanup performs cleanup operations.
func (c *BrowserStreamClient) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stream != nil {
		_ = c.stream.CloseSend()
		c.stream = nil
	}

	if c.provider != nil {
		_ = c.provider.Stop(context.Background())
	}

	c.logger.Info("browser stream stopped", zap.String("tunnel_id", c.tunnelID))
}

// SendStateUpdate sends a state update to the server.
func (c *BrowserStreamClient) SendStateUpdate(state string, stateMsg string) error {
	c.mu.Lock()
	stream := c.stream
	c.mu.Unlock()

	if stream == nil {
		return fmt.Errorf("stream not connected")
	}

	msg := &pb.RunnerBrowserMessage{
		Payload: &pb.RunnerBrowserMessage_State{
			State: &pb.BrowserStreamState{
				State: state,
				Error: stateMsg,
			},
		},
	}

	return stream.Send(msg)
}
