package permission

import (
	"bytes"
	"context"
	"sync"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"go.uber.org/zap"
)

// Detector wraps an OutputHandler to detect and handle permission requests.
type Detector struct {
	handler executor.OutputHandler
	parser  *Parser
	logger  *zap.Logger

	mu             sync.Mutex
	pendingRequest *executor.PermissionRequest
}

// NewDetector creates a new permission detector that wraps the given handler.
func NewDetector(handler executor.OutputHandler, logger *zap.Logger) *Detector {
	return &Detector{
		handler: handler,
		parser:  NewParser(),
		logger:  logger.Named("permission-detector"),
	}
}

// HandleOutput processes output and detects permission requests.
// If a permission request is detected, it will be sent to the underlying handler.
func (d *Detector) HandleOutput(stream string, data []byte) {
	// Always forward output to the underlying handler
	d.handler.HandleOutput(stream, data)

	// Only parse stdout for permission requests
	if stream != "stdout" {
		return
	}

	// Parse each line for permission requests
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		if req := d.parser.ParseLine(string(line)); req != nil {
			d.mu.Lock()
			d.pendingRequest = req
			d.mu.Unlock()

			d.logger.Info("permission request detected",
				zap.String("request_id", req.ID),
				zap.String("tool", req.Tool),
				zap.String("action", req.Action),
				zap.String("risk_level", req.RiskLevel.String()),
			)
		}
	}
}

// HandlePermissionRequest forwards permission requests to the underlying handler.
func (d *Detector) HandlePermissionRequest(ctx context.Context, req *executor.PermissionRequest) (bool, error) {
	return d.handler.HandlePermissionRequest(ctx, req)
}

// GetPendingRequest returns the current pending permission request, if any.
func (d *Detector) GetPendingRequest() *executor.PermissionRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pendingRequest
}

// ClearPendingRequest clears the pending permission request.
func (d *Detector) ClearPendingRequest() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pendingRequest = nil
	d.parser.Reset()
}

// HasPendingRequest returns true if there's a pending permission request.
func (d *Detector) HasPendingRequest() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pendingRequest != nil
}

// WaitForPermission blocks until a permission response is received for the pending request.
// Returns true if approved, false if denied.
// Returns error if context is cancelled or no pending request exists.
func (d *Detector) WaitForPermission(ctx context.Context) (bool, error) {
	d.mu.Lock()
	req := d.pendingRequest
	d.mu.Unlock()

	if req == nil {
		return false, nil
	}

	// Send permission request to handler
	d.logger.Debug("sending permission request to handler",
		zap.String("request_id", req.ID),
	)

	approved, err := d.handler.HandlePermissionRequest(ctx, req)
	if err != nil {
		d.logger.Error("permission request failed",
			zap.String("request_id", req.ID),
			zap.Error(err),
		)
		return false, err
	}

	d.logger.Info("permission response received",
		zap.String("request_id", req.ID),
		zap.Bool("approved", approved),
	)

	// Clear pending request after response
	d.ClearPendingRequest()

	return approved, nil
}

// Reset resets the detector state.
func (d *Detector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pendingRequest = nil
	d.parser.Reset()
}
