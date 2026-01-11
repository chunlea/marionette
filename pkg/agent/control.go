package agent

import (
	"context"
	"fmt"
	"io"
	"sync"
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

	stream   pb.RunnerService_ConnectClient
	streamMu sync.Mutex

	// Channel for outgoing messages
	outbox chan *pb.RunnerMessage

	stopC    chan struct{}
	stoppedC chan struct{}
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

	// Create the bidirectional stream
	stream, err := c.client.GRPCClient().Connect(c.client.AttachMetadata(ctx))
	if err != nil {
		return fmt.Errorf("creating control stream: %w", err)
	}

	c.streamMu.Lock()
	c.stream = stream
	c.streamMu.Unlock()

	c.logger.Info("control channel established")

	// Start the receive and send loops
	go c.run(ctx)

	return nil
}

// run manages the control channel lifecycle.
func (c *ControlChannel) run(ctx context.Context) {
	defer close(c.stoppedC)

	// Start sender goroutine
	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		c.sendLoop(ctx)
	}()

	// Run receiver in main goroutine
	c.receiveLoop(ctx)

	// Wait for sender to finish
	<-senderDone
}

// receiveLoop receives commands from the server.
func (c *ControlChannel) receiveLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
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
		go c.handleCommand(ctx, cmd)
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

// Stop stops the control channel.
func (c *ControlChannel) Stop() {
	close(c.stopC)
	c.Wait()
}

// StopAsync signals the control channel to stop without waiting.
func (c *ControlChannel) StopAsync() {
	select {
	case <-c.stopC:
		// Already closed
	default:
		close(c.stopC)
	}
}

// Wait blocks until the control channel has stopped.
func (c *ControlChannel) Wait() {
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
	default:
		return "Unknown"
	}
}
