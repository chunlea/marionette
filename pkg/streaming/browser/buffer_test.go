package browser

import (
	"sync"
	"testing"
	"time"
)

func TestNewFrameBuffer(t *testing.T) {
	tests := []struct {
		name    string
		cfg     FrameBufferConfig
		wantCap int
		wantPol DropPolicy
	}{
		{
			name:    "default capacity",
			cfg:     FrameBufferConfig{},
			wantCap: DefaultBufferSize,
			wantPol: DropPolicyOldest,
		},
		{
			name:    "custom capacity",
			cfg:     FrameBufferConfig{Capacity: 20},
			wantCap: 20,
			wantPol: DropPolicyOldest,
		},
		{
			name:    "negative capacity uses default",
			cfg:     FrameBufferConfig{Capacity: -5},
			wantCap: DefaultBufferSize,
			wantPol: DropPolicyOldest,
		},
		{
			name:    "exceeds max capacity",
			cfg:     FrameBufferConfig{Capacity: MaxBufferSize + 100},
			wantCap: MaxBufferSize,
			wantPol: DropPolicyOldest,
		},
		{
			name:    "custom drop policy",
			cfg:     FrameBufferConfig{Capacity: 10, DropPolicy: DropPolicyNewest},
			wantCap: 10,
			wantPol: DropPolicyNewest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewFrameBuffer(tt.cfg)
			if buf.Cap() != tt.wantCap {
				t.Errorf("Cap() = %d, want %d", buf.Cap(), tt.wantCap)
			}
			stats := buf.Stats()
			if stats.DropPolicy != tt.wantPol {
				t.Errorf("DropPolicy = %d, want %d", stats.DropPolicy, tt.wantPol)
			}
		})
	}
}

func TestFrameBuffer_PushPop(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 3})

	frame1 := &Frame{Sequence: 1}
	frame2 := &Frame{Sequence: 2}
	frame3 := &Frame{Sequence: 3}

	// Push frames
	if err := buf.Push(frame1); err != nil {
		t.Errorf("Push() error = %v", err)
	}
	if err := buf.Push(frame2); err != nil {
		t.Errorf("Push() error = %v", err)
	}
	if err := buf.Push(frame3); err != nil {
		t.Errorf("Push() error = %v", err)
	}

	if buf.Len() != 3 {
		t.Errorf("Len() = %d, want 3", buf.Len())
	}

	// Pop frames (FIFO order)
	popped := buf.Pop()
	if popped.Sequence != 1 {
		t.Errorf("Pop() Sequence = %d, want 1", popped.Sequence)
	}

	popped = buf.Pop()
	if popped.Sequence != 2 {
		t.Errorf("Pop() Sequence = %d, want 2", popped.Sequence)
	}

	popped = buf.Pop()
	if popped.Sequence != 3 {
		t.Errorf("Pop() Sequence = %d, want 3", popped.Sequence)
	}

	// Pop from empty buffer
	if buf.Pop() != nil {
		t.Error("Pop() from empty buffer should return nil")
	}
}

func TestFrameBuffer_DropPolicyNewest(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   2,
		DropPolicy: DropPolicyNewest,
	})

	frame1 := &Frame{Sequence: 1}
	frame2 := &Frame{Sequence: 2}
	frame3 := &Frame{Sequence: 3}

	// Fill buffer
	_ = buf.Push(frame1)
	_ = buf.Push(frame2)

	// Push when full should fail
	err := buf.Push(frame3)
	if err != ErrBufferFull {
		t.Errorf("Push() error = %v, want ErrBufferFull", err)
	}

	// Oldest frames should still be there
	if buf.Pop().Sequence != 1 {
		t.Error("frame1 should still be in buffer")
	}
	if buf.Pop().Sequence != 2 {
		t.Error("frame2 should still be in buffer")
	}

	// Check dropped count
	if buf.DroppedFrames() != 1 {
		t.Errorf("DroppedFrames() = %d, want 1", buf.DroppedFrames())
	}
}

func TestFrameBuffer_DropPolicyOldest(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   2,
		DropPolicy: DropPolicyOldest,
	})

	frame1 := &Frame{Sequence: 1}
	frame2 := &Frame{Sequence: 2}
	frame3 := &Frame{Sequence: 3}

	// Fill buffer
	_ = buf.Push(frame1)
	_ = buf.Push(frame2)

	// Push when full should drop oldest and add new
	err := buf.Push(frame3)
	if err != nil {
		t.Errorf("Push() error = %v, want nil", err)
	}

	// Newest frames should be there (2 and 3)
	got := buf.Pop()
	if got.Sequence != 2 {
		t.Errorf("Pop() Sequence = %d, want 2", got.Sequence)
	}
	got = buf.Pop()
	if got.Sequence != 3 {
		t.Errorf("Pop() Sequence = %d, want 3", got.Sequence)
	}

	// Check dropped count
	if buf.DroppedFrames() != 1 {
		t.Errorf("DroppedFrames() = %d, want 1", buf.DroppedFrames())
	}
}

func TestFrameBuffer_DropPolicyBlock(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   1,
		DropPolicy: DropPolicyBlock,
	})

	frame1 := &Frame{Sequence: 1}
	frame2 := &Frame{Sequence: 2}

	// Push first frame
	_ = buf.Push(frame1)

	// Start consumer
	done := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		buf.Pop()
		close(done)
	}()

	// This should block until Pop() is called
	start := time.Now()
	err := buf.Push(frame2)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Push() error = %v", err)
	}
	if elapsed < 5*time.Millisecond {
		t.Error("Push() should have blocked")
	}

	<-done
}

func TestFrameBuffer_IsFullIsEmpty(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 2})

	if !buf.IsEmpty() {
		t.Error("IsEmpty() = false, want true")
	}
	if buf.IsFull() {
		t.Error("IsFull() = true, want false")
	}

	_ = buf.Push(&Frame{Sequence: 1})

	if buf.IsEmpty() {
		t.Error("IsEmpty() = true, want false")
	}
	if buf.IsFull() {
		t.Error("IsFull() = true, want false")
	}

	_ = buf.Push(&Frame{Sequence: 2})

	if buf.IsEmpty() {
		t.Error("IsEmpty() = true, want false")
	}
	if !buf.IsFull() {
		t.Error("IsFull() = false, want true")
	}
}

func TestFrameBuffer_Clear(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})

	for i := 0; i < 5; i++ {
		_ = buf.Push(&Frame{Sequence: uint64(i)})
	}

	if buf.Len() != 5 {
		t.Errorf("Len() = %d, want 5", buf.Len())
	}

	buf.Clear()

	if buf.Len() != 0 {
		t.Errorf("Len() after Clear() = %d, want 0", buf.Len())
	}
	if !buf.IsEmpty() {
		t.Error("IsEmpty() after Clear() = false, want true")
	}
}

func TestFrameBuffer_Close(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})
	_ = buf.Push(&Frame{Sequence: 1})

	if buf.IsClosed() {
		t.Error("IsClosed() = true before Close()")
	}

	err := buf.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if !buf.IsClosed() {
		t.Error("IsClosed() = false after Close()")
	}

	// Push after close should fail
	err = buf.Push(&Frame{Sequence: 2})
	if err != ErrProviderClosed {
		t.Errorf("Push() after Close() error = %v, want ErrProviderClosed", err)
	}

	// Double close should be safe
	err = buf.Close()
	if err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}

func TestFrameBuffer_Frames(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 3})

	// Push some frames
	for i := 1; i <= 3; i++ {
		_ = buf.Push(&Frame{Sequence: uint64(i)})
	}

	// Read from channel
	ch := buf.Frames()
	for i := 1; i <= 3; i++ {
		select {
		case frame := <-ch:
			if frame.Sequence != uint64(i) {
				t.Errorf("frame.Sequence = %d, want %d", frame.Sequence, i)
			}
		default:
			t.Errorf("expected frame %d", i)
		}
	}
}

func TestFrameBuffer_Stats(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   2,
		DropPolicy: DropPolicyNewest,
	})

	_ = buf.Push(&Frame{Sequence: 1})
	_ = buf.Push(&Frame{Sequence: 2})
	_ = buf.Push(&Frame{Sequence: 3}) // dropped

	stats := buf.Stats()

	if stats.Capacity != 2 {
		t.Errorf("Capacity = %d, want 2", stats.Capacity)
	}
	if stats.Length != 2 {
		t.Errorf("Length = %d, want 2", stats.Length)
	}
	if stats.TotalFrames != 3 {
		t.Errorf("TotalFrames = %d, want 3", stats.TotalFrames)
	}
	if stats.DroppedFrames != 1 {
		t.Errorf("DroppedFrames = %d, want 1", stats.DroppedFrames)
	}
}

func TestFrameBufferStats_DropRate(t *testing.T) {
	tests := []struct {
		name  string
		stats FrameBufferStats
		want  float64
	}{
		{
			name:  "no frames",
			stats: FrameBufferStats{TotalFrames: 0, DroppedFrames: 0},
			want:  0,
		},
		{
			name:  "no drops",
			stats: FrameBufferStats{TotalFrames: 100, DroppedFrames: 0},
			want:  0,
		},
		{
			name:  "50% drop",
			stats: FrameBufferStats{TotalFrames: 100, DroppedFrames: 50},
			want:  50,
		},
		{
			name:  "all dropped",
			stats: FrameBufferStats{TotalFrames: 100, DroppedFrames: 100},
			want:  100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stats.DropRate(); got != tt.want {
				t.Errorf("DropRate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFrameBufferStats_Utilization(t *testing.T) {
	tests := []struct {
		name  string
		stats FrameBufferStats
		want  float64
	}{
		{
			name:  "zero capacity",
			stats: FrameBufferStats{Capacity: 0, Length: 0},
			want:  0,
		},
		{
			name:  "empty buffer",
			stats: FrameBufferStats{Capacity: 10, Length: 0},
			want:  0,
		},
		{
			name:  "50% full",
			stats: FrameBufferStats{Capacity: 10, Length: 5},
			want:  50,
		},
		{
			name:  "full buffer",
			stats: FrameBufferStats{Capacity: 10, Length: 10},
			want:  100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stats.Utilization(); got != tt.want {
				t.Errorf("Utilization() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFrameBuffer_Concurrent(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   10,
		DropPolicy: DropPolicyNewest,
	})

	var wg sync.WaitGroup
	numWriters := 5
	numFramesPerWriter := 100
	totalFrames := numWriters * numFramesPerWriter

	// Start writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < numFramesPerWriter; i++ {
				frame := &Frame{Sequence: uint64(writerID*numFramesPerWriter + i)}
				_ = buf.Push(frame)
			}
		}(w)
	}

	// Wait for all writers to complete
	wg.Wait()

	// Drain the buffer (read whatever is left)
	readCount := 0
	for buf.Pop() != nil {
		readCount++
	}

	stats := buf.Stats()

	// All frames should be accounted for
	if stats.TotalFrames != uint64(totalFrames) {
		t.Errorf("TotalFrames = %d, want %d", stats.TotalFrames, totalFrames)
	}

	// With DropPolicyNewest, many frames will be dropped when buffer is full
	// readCount + droppedFrames should account for frames not currently in buffer
	t.Logf("TotalFrames=%d, DroppedFrames=%d, ReadCount=%d", stats.TotalFrames, stats.DroppedFrames, readCount)
}

func TestFrameBuffer_TotalFrames(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})

	for i := 0; i < 10; i++ {
		_ = buf.Push(&Frame{Sequence: uint64(i)})
		buf.Pop()
	}

	if buf.TotalFrames() != 10 {
		t.Errorf("TotalFrames() = %d, want 10", buf.TotalFrames())
	}
}
