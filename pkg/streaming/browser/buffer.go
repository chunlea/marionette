// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package browser

import (
	"sync"
	"sync/atomic"
)

// FrameBuffer is a thread-safe buffer for browser frames.
// It implements a bounded buffer with configurable backpressure handling.
type FrameBuffer struct {
	capacity int
	frames   chan *Frame
	closed   atomic.Bool

	mu            sync.RWMutex
	droppedFrames uint64
	totalFrames   uint64
	dropPolicy    DropPolicy
}

// DropPolicy defines how the buffer handles overflow.
type DropPolicy int

const (
	// DropPolicyOldest drops the oldest frame when buffer is full.
	DropPolicyOldest DropPolicy = iota
	// DropPolicyNewest drops the newest (incoming) frame when buffer is full.
	DropPolicyNewest
	// DropPolicyBlock blocks until space is available (may cause backpressure).
	DropPolicyBlock
)

// FrameBufferConfig configures the frame buffer.
type FrameBufferConfig struct {
	// Capacity is the maximum number of frames to buffer.
	// Default: DefaultBufferSize
	Capacity int

	// DropPolicy specifies how to handle overflow.
	// Default: DropPolicyNewest
	DropPolicy DropPolicy
}

// NewFrameBuffer creates a new frame buffer with the given configuration.
func NewFrameBuffer(cfg FrameBufferConfig) *FrameBuffer {
	if cfg.Capacity <= 0 {
		cfg.Capacity = DefaultBufferSize
	}
	if cfg.Capacity > MaxBufferSize {
		cfg.Capacity = MaxBufferSize
	}

	return &FrameBuffer{
		capacity:   cfg.Capacity,
		frames:     make(chan *Frame, cfg.Capacity),
		dropPolicy: cfg.DropPolicy,
	}
}

// Push adds a frame to the buffer.
// Behavior depends on the DropPolicy:
// - DropPolicyNewest: returns ErrBufferFull and drops the frame
// - DropPolicyOldest: drops the oldest frame and adds the new one
// - DropPolicyBlock: blocks until space is available
func (b *FrameBuffer) Push(frame *Frame) error {
	if b.closed.Load() {
		return ErrProviderClosed
	}

	b.mu.Lock()
	b.totalFrames++
	b.mu.Unlock()

	switch b.dropPolicy {
	case DropPolicyBlock:
		b.frames <- frame
		return nil

	case DropPolicyNewest:
		select {
		case b.frames <- frame:
			return nil
		default:
			b.mu.Lock()
			b.droppedFrames++
			b.mu.Unlock()
			return ErrBufferFull
		}

	case DropPolicyOldest:
		select {
		case b.frames <- frame:
			return nil
		default:
			// Buffer is full, drop oldest
			select {
			case <-b.frames:
				b.mu.Lock()
				b.droppedFrames++
				b.mu.Unlock()
			default:
				// Channel was drained by a reader
			}
			// Try to add new frame
			select {
			case b.frames <- frame:
				return nil
			default:
				// Still full (concurrent readers drained it differently)
				b.mu.Lock()
				b.droppedFrames++
				b.mu.Unlock()
				return ErrBufferFull
			}
		}

	default:
		// Default to DropPolicyNewest
		select {
		case b.frames <- frame:
			return nil
		default:
			b.mu.Lock()
			b.droppedFrames++
			b.mu.Unlock()
			return ErrBufferFull
		}
	}
}

// Pop removes and returns the oldest frame from the buffer.
// Returns nil if the buffer is empty.
func (b *FrameBuffer) Pop() *Frame {
	select {
	case frame := <-b.frames:
		return frame
	default:
		return nil
	}
}

// Frames returns the channel for receiving frames.
// The channel is closed when Close() is called.
func (b *FrameBuffer) Frames() <-chan *Frame {
	return b.frames
}

// Len returns the current number of frames in the buffer.
func (b *FrameBuffer) Len() int {
	return len(b.frames)
}

// Cap returns the buffer capacity.
func (b *FrameBuffer) Cap() int {
	return b.capacity
}

// IsFull returns true if the buffer is at capacity.
func (b *FrameBuffer) IsFull() bool {
	return len(b.frames) >= b.capacity
}

// IsEmpty returns true if the buffer is empty.
func (b *FrameBuffer) IsEmpty() bool {
	return len(b.frames) == 0
}

// Stats returns buffer statistics.
func (b *FrameBuffer) Stats() FrameBufferStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return FrameBufferStats{
		Capacity:      b.capacity,
		Length:        len(b.frames),
		TotalFrames:   b.totalFrames,
		DroppedFrames: b.droppedFrames,
		DropPolicy:    b.dropPolicy,
	}
}

// DroppedFrames returns the number of dropped frames.
func (b *FrameBuffer) DroppedFrames() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.droppedFrames
}

// TotalFrames returns the total number of frames pushed.
func (b *FrameBuffer) TotalFrames() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.totalFrames
}

// Clear removes all frames from the buffer.
func (b *FrameBuffer) Clear() {
	for {
		select {
		case <-b.frames:
		default:
			return
		}
	}
}

// Close closes the buffer.
// After Close, Push returns ErrProviderClosed and the Frames channel is closed.
func (b *FrameBuffer) Close() error {
	if b.closed.CompareAndSwap(false, true) {
		close(b.frames)
	}
	return nil
}

// IsClosed returns true if the buffer has been closed.
func (b *FrameBuffer) IsClosed() bool {
	return b.closed.Load()
}

// FrameBufferStats contains statistics about a frame buffer.
type FrameBufferStats struct {
	// Capacity is the buffer capacity.
	Capacity int `json:"capacity"`

	// Length is the current number of buffered frames.
	Length int `json:"length"`

	// TotalFrames is the total number of frames pushed.
	TotalFrames uint64 `json:"total_frames"`

	// DroppedFrames is the number of dropped frames.
	DroppedFrames uint64 `json:"dropped_frames"`

	// DropPolicy is the buffer's drop policy.
	DropPolicy DropPolicy `json:"drop_policy"`
}

// DropRate returns the frame drop rate as a percentage (0-100).
func (s FrameBufferStats) DropRate() float64 {
	if s.TotalFrames == 0 {
		return 0
	}
	return float64(s.DroppedFrames) / float64(s.TotalFrames) * 100
}

// Utilization returns the buffer utilization as a percentage (0-100).
func (s FrameBufferStats) Utilization() float64 {
	if s.Capacity == 0 {
		return 0
	}
	return float64(s.Length) / float64(s.Capacity) * 100
}
