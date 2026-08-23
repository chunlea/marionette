package agent

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
)

// ControlChannel manages the bidirectional streaming connection with the server.
// It receives ServerCommand messages and dispatches them to appropriate handlers.
type ControlChannel struct {
	client  *Client
	handler CommandHandler
	logger  *zap.Logger

	stream       pb.RunnerService_ConnectClient
	streamCancel context.CancelFunc
	streamMu     sync.Mutex

	// Channel for outgoing messages
	outbox chan *pb.RunnerMessage

	stopC    chan struct{}
	stoppedC chan struct{}
	stopOnce sync.Once

	// started is set once run() is on its way, so Stop and Wait know whether
	// anything will ever close stoppedC.
	started atomic.Bool
}

// NewControlChannel creates a new control channel.
func NewControlChannel(client *Client, handler CommandHandler, logger *zap.Logger) *ControlChannel {
	return &ControlChannel{
		client:   client,
		handler:  handler,
		logger:   logger.Named("control"),
		outbox:   make(chan *pb.RunnerMessage, 100),
		stopC:    make(chan struct{}),
		stoppedC: make(chan struct{}),
	}
}

// Start establishes the bidirectional stream and begins processing.
func (c *ControlChannel) Start(ctx context.Context) error {
	if c.client.State() != StateConnected {
		return fmt.Errorf("client not connected")
	}

	// The stream gets its own cancelable context so Stop can interrupt a
	// blocked Recv. Commands keep running on the caller's context: tearing the
	// connection down must not cancel a task that is already executing.
	streamCtx, streamCancel := context.WithCancel(ctx)

	stream, err := c.client.GRPCClient().Connect(c.client.AttachMetadata(streamCtx))
	if err != nil {
		streamCancel()
		return fmt.Errorf("creating control stream: %w", err)
	}

	c.streamMu.Lock()
	c.stream = stream
	c.streamCancel = streamCancel
	c.streamMu.Unlock()

	c.logger.Info("control channel established")

	// Start the receive and send loops
	c.started.Store(true)
	go c.run(ctx, streamCtx)

	return nil
}

// run manages the control channel lifecycle.
//
// cmdCtx is the caller's context and is what dispatched commands run on.
// streamCtx additionally dies when Stop is called, which is what unblocks a
// receive sitting in stream.Recv.
func (c *ControlChannel) run(cmdCtx, streamCtx context.Context) {
	defer close(c.stoppedC)

	// Start sender goroutine
	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		c.sendLoop(streamCtx)
	}()

	// Run receiver in main goroutine
	c.receiveLoop(cmdCtx, streamCtx)

	// receiveLoop only returns when this channel is finished: stop requested,
	// context canceled, or the stream failed. In the stream-failure case
	// nothing else ever releases sendLoop, which selects on ctx and stopC
	// only - so run() would block on senderDone forever, stoppedC would never
	// close, and every caller of Wait (including the reconnect supervisor)
	// would hang behind a connection that is already gone.
	c.StopAsync()

	// Wait for sender to finish
	<-senderDone
}

// receiveLoop receives commands from the server.
func (c *ControlChannel) receiveLoop(cmdCtx, streamCtx context.Context) {
	for {
		select {
		case <-streamCtx.Done():
			c.logger.Info("receive loop stopped: context canceled")
			return
		case <-c.stopC:
			c.logger.Info("receive loop stopped: stop requested")
			return
		default:
		}

		c.streamMu.Lock()
		stream := c.stream
		c.streamMu.Unlock()

		if stream == nil {
			c.logger.Debug("stream not available, waiting...")
			time.Sleep(100 * time.Millisecond)
			continue
		}

		cmd, err := stream.Recv()
		if err == io.EOF {
			c.logger.Info("server closed control stream")
			return
		}
		if err != nil {
			c.logger.Error("error receiving command", zap.Error(err))
			return
		}

		// Dispatch command to handler
		go c.handleCommand(cmdCtx, cmd)
	}
}

// sendLoop sends messages to the server.
func (c *ControlChannel) sendLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopC:
			return
		case msg := <-c.outbox:
			c.streamMu.Lock()
			stream := c.stream
			c.streamMu.Unlock()

			if stream == nil {
				c.logger.Warn("cannot send message: stream not available")
				continue
			}

			if err := stream.Send(msg); err != nil {
				c.logger.Error("error sending message", zap.Error(err))
			}
		}
	}
}

// handleCommand dispatches a command to the appropriate handler.
func (c *ControlChannel) handleCommand(ctx context.Context, cmd *pb.ServerCommand) {
	c.logger.Debug("received command", zap.String("type", commandType(cmd)))

	var response *pb.RunnerMessage
	var err error

	switch payload := cmd.Payload.(type) {
	case *pb.ServerCommand_ExecuteTask:
		response, err = c.handler.HandleExecuteTask(ctx, payload.ExecuteTask)
	case *pb.ServerCommand_ApprovePermission:
		response, err = c.handler.HandleApprovePermission(ctx, payload.ApprovePermission)
	case *pb.ServerCommand_KillTask:
		response, err = c.handler.HandleKillTask(ctx, payload.KillTask)
	case *pb.ServerCommand_CreateTunnel:
		response, err = c.handler.HandleCreateTunnel(ctx, payload.CreateTunnel)
	case *pb.ServerCommand_TunnelData:
		response, err = c.handler.HandleTunnelData(ctx, payload.TunnelData)
	case *pb.ServerCommand_AttachSession:
		response, err = c.handler.HandleAttachSession(ctx, payload.AttachSession)
	case *pb.ServerCommand_DetachSession:
		response, err = c.handler.HandleDetachSession(ctx, payload.DetachSession)
	case *pb.ServerCommand_StartDesktopStream:
		response, err = c.handler.HandleStartDesktopStream(ctx, payload.StartDesktopStream)
	case *pb.ServerCommand_StopDesktopStream:
		response, err = c.handler.HandleStopDesktopStream(ctx, payload.StopDesktopStream)
	default:
		c.logger.Warn("unknown command type", zap.Any("payload", cmd.Payload))
		return
	}

	if err != nil {
		c.logger.Error("error handling command",
			zap.String("type", commandType(cmd)),
			zap.Error(err),
		)
	}

	// Send response if provided
	if response != nil {
		c.Send(response)
	}
}

// Send queues a message to be sent to the server.
func (c *ControlChannel) Send(msg *pb.RunnerMessage) {
	select {
	case c.outbox <- msg:
	default:
		c.logger.Warn("outbox full, dropping message")
	}
}

// Stop stops the control channel and waits for it to finish.
//
// It is safe to call more than once, and safe to call when Start never ran or
// failed: an unconditional close(c.stopC) panicked on the second call, and
// waiting on stoppedC deadlocked when no run() goroutine existed to close it.
func (c *ControlChannel) Stop() {
	c.StopAsync()
	c.Wait()
}

// StopAsync signals the control channel to stop without waiting.
func (c *ControlChannel) StopAsync() {
	c.stopOnce.Do(func() {
		close(c.stopC)

		// Cancel the stream too. stopC alone cannot interrupt a receive that
		// is blocked in stream.Recv, so without this Stop would hang until the
		// connection happened to break on its own.
		c.streamMu.Lock()
		cancel := c.streamCancel
		c.streamMu.Unlock()

		if cancel != nil {
			cancel()
		}
	})
}

// Wait blocks until the control channel has stopped. It returns immediately if
// the channel was never started, since nothing will close stoppedC.
func (c *ControlChannel) Wait() {
	if !c.started.Load() {
		return
	}
	<-c.stoppedC
}

// Stream returns the current stream for direct access (e.g., heartbeat loop).
func (c *ControlChannel) Stream() pb.RunnerService_ConnectClient {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	return c.stream
}

// commandType returns a string representation of the command type.
func commandType(cmd *pb.ServerCommand) string {
	switch cmd.Payload.(type) {
	case *pb.ServerCommand_ExecuteTask:
		return "ExecuteTask"
	case *pb.ServerCommand_ApprovePermission:
		return "ApprovePermission"
	case *pb.ServerCommand_KillTask:
		return "KillTask"
	case *pb.ServerCommand_CreateTunnel:
		return "CreateTunnel"
	case *pb.ServerCommand_TunnelData:
		return "TunnelData"
	case *pb.ServerCommand_AttachSession:
		return "AttachSession"
	case *pb.ServerCommand_DetachSession:
		return "DetachSession"
	case *pb.ServerCommand_StartDesktopStream:
		return "StartDesktopStream"
	case *pb.ServerCommand_StopDesktopStream:
		return "StopDesktopStream"
	default:
		return "Unknown"
	}
}
