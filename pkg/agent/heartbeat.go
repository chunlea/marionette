package agent

import (
	"context"
	"math"
	"runtime"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
)

// ResourceUsage contains resource usage metrics for heartbeat.
type ResourceUsage struct {
	CPUPercent  float64
	MemoryBytes int64
	DiskBytes   int64
}

// HeartbeatLoop manages periodic heartbeat sending to the server.
type HeartbeatLoop struct {
	client *Client
	cfg    HeartbeatConfig
	logger *zap.Logger

	startTime time.Time
	status    string
	statusMu  sync.RWMutex

	stream   pb.RunnerService_ConnectClient
	streamMu sync.Mutex

	stopC    chan struct{}
	stoppedC chan struct{}
}

// NewHeartbeatLoop creates a new heartbeat loop.
func NewHeartbeatLoop(client *Client, cfg HeartbeatConfig, logger *zap.Logger) *HeartbeatLoop {
	return &HeartbeatLoop{
		client:    client,
		cfg:       cfg,
		logger:    logger.Named("heartbeat"),
		startTime: time.Now(),
		status:    "idle",
		stopC:     make(chan struct{}),
		stoppedC:  make(chan struct{}),
	}
}

// Start begins the heartbeat loop in a goroutine.
func (h *HeartbeatLoop) Start(ctx context.Context) {
	go h.run(ctx)
}

func (h *HeartbeatLoop) run(ctx context.Context) {
	defer close(h.stoppedC)

	ticker := time.NewTicker(h.cfg.Interval)
	defer ticker.Stop()

	// Send initial heartbeat
	h.sendHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("heartbeat loop stopped: context canceled")
			return
		case <-h.stopC:
			h.logger.Info("heartbeat loop stopped: stop requested")
			return
		case <-ticker.C:
			h.sendHeartbeat(ctx)
		}
	}
}

func (h *HeartbeatLoop) sendHeartbeat(_ context.Context) {
	if h.client.State() != StateConnected {
		h.logger.Debug("skipping heartbeat: not connected")
		return
	}

	resources := h.collectResources()
	status := h.getStatus()
	uptime := h.uptimeSeconds()

	h.logger.Debug("sending heartbeat",
		zap.String("status", status),
		zap.Int64("uptime_seconds", uptime),
		zap.Float64("cpu_percent", resources.CPUPercent),
		zap.Int64("memory_bytes", resources.MemoryBytes),
	)

	// For Phase 1 (Basic), we log the heartbeat.
	// In Phase 2 (Control Channel), we'll send via the Connect stream.
	// The actual sending is done here as a placeholder for future implementation.

	// Create the heartbeat message
	hb := &pb.Heartbeat{
		RunnerId:      h.client.RunnerID(),
		Status:        status,
		UptimeSeconds: uptime,
		Resources: &pb.ResourceUsage{
			CpuPercent:  resources.CPUPercent,
			MemoryBytes: resources.MemoryBytes,
			DiskBytes:   resources.DiskBytes,
		},
	}

	// Try to send via stream if available
	h.streamMu.Lock()
	stream := h.stream
	h.streamMu.Unlock()

	if stream != nil {
		msg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_Heartbeat{
				Heartbeat: hb,
			},
		}
		if err := stream.Send(msg); err != nil {
			h.logger.Warn("failed to send heartbeat", zap.Error(err))
		}
	}
}

func (h *HeartbeatLoop) collectResources() *ResourceUsage {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Safely convert uint64 to int64, capping at max int64 value
	memBytes := m.Alloc
	if memBytes > math.MaxInt64 {
		memBytes = math.MaxInt64
	}

	return &ResourceUsage{
		CPUPercent:  0,               // TODO: Implement CPU monitoring in future phase
		MemoryBytes: int64(memBytes), //nolint:gosec // Safe: capped at MaxInt64 above
		DiskBytes:   0,               // TODO: Implement disk monitoring in future phase
	}
}

// SetStatus sets the current runner status (idle, busy, paused).
func (h *HeartbeatLoop) SetStatus(status string) {
	h.statusMu.Lock()
	h.status = status
	h.statusMu.Unlock()
}

func (h *HeartbeatLoop) getStatus() string {
	h.statusMu.RLock()
	defer h.statusMu.RUnlock()
	return h.status
}

func (h *HeartbeatLoop) uptimeSeconds() int64 {
	return int64(time.Since(h.startTime).Seconds())
}

// SetStream sets the bidirectional stream for sending heartbeats.
// This will be used when the Control Channel is implemented.
func (h *HeartbeatLoop) SetStream(stream pb.RunnerService_ConnectClient) {
	h.streamMu.Lock()
	h.stream = stream
	h.streamMu.Unlock()
}

// Stop stops the heartbeat loop and waits for it to finish.
func (h *HeartbeatLoop) Stop() {
	close(h.stopC)
	<-h.stoppedC
}

// StopAsync stops the heartbeat loop without waiting.
func (h *HeartbeatLoop) StopAsync() {
	close(h.stopC)
}

// Wait waits for the heartbeat loop to stop.
func (h *HeartbeatLoop) Wait() {
	<-h.stoppedC
}
