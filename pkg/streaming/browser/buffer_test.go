// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package browser

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFrameBuffer(t *testing.T) {
	t.Run("default capacity", func(t *testing.T) {
		buf := NewFrameBuffer(FrameBufferConfig{})
		assert.Equal(t, DefaultBufferSize, buf.Cap())
	})

	t.Run("custom capacity", func(t *testing.T) {
		buf := NewFrameBuffer(FrameBufferConfig{Capacity: 10})
		assert.Equal(t, 10, buf.Cap())
	})

	t.Run("negative capacity uses default", func(t *testing.T) {
		buf := NewFrameBuffer(FrameBufferConfig{Capacity: -1})
		assert.Equal(t, DefaultBufferSize, buf.Cap())
	})

	t.Run("exceeds max capacity", func(t *testing.T) {
		buf := NewFrameBuffer(FrameBufferConfig{Capacity: MaxBufferSize + 100})
		assert.Equal(t, MaxBufferSize, buf.Cap())
	})

	t.Run("with drop policy", func(t *testing.T) {
		buf := NewFrameBuffer(FrameBufferConfig{
			Capacity:   5,
			DropPolicy: DropPolicyOldest,
		})
		stats := buf.Stats()
		assert.Equal(t, DropPolicyOldest, stats.DropPolicy)
	})
}

func TestFrameBuffer_PushPop(t *testing.T) {
	t.Run("push and pop", func(t *testing.T) {
		buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})

		frame := &Frame{Sequence: 1, Data: []byte("test")}
		err := buf.Push(frame)
		require.NoError(t, err)

		popped := buf.Pop()
		assert.Equal(t, frame, popped)
	})

	t.Run("pop from empty", func(t *testing.T) {
		buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})
		popped := buf.Pop()
		assert.Nil(t, popped)
	})

	t.Run("push to closed buffer", func(t *testing.T) {
		buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})
		require.NoError(t, buf.Close())

		err := buf.Push(&Frame{})
		assert.Equal(t, ErrProviderClosed, err)
	})
}

func TestFrameBuffer_DropPolicyNewest(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   2,
		DropPolicy: DropPolicyNewest,
	})

	// Fill buffer
	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	require.NoError(t, buf.Push(&Frame{Sequence: 2}))

	// Try to add third frame - should be dropped
	err := buf.Push(&Frame{Sequence: 3})
	assert.Equal(t, ErrBufferFull, err)

	// Verify dropped count
	assert.Equal(t, uint64(1), buf.DroppedFrames())
	assert.Equal(t, uint64(3), buf.TotalFrames())

	// Verify oldest frames are still there
	frame := buf.Pop()
	assert.Equal(t, uint64(1), frame.Sequence)
}

func TestFrameBuffer_DropPolicyOldest(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   2,
		DropPolicy: DropPolicyOldest,
	})

	// Fill buffer
	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	require.NoError(t, buf.Push(&Frame{Sequence: 2}))

	// Add third frame - oldest should be dropped
	err := buf.Push(&Frame{Sequence: 3})
	require.NoError(t, err)

	// Verify dropped count
	assert.Equal(t, uint64(1), buf.DroppedFrames())

	// Verify newest frames are there
	frame := buf.Pop()
	assert.Equal(t, uint64(2), frame.Sequence)

	frame = buf.Pop()
	assert.Equal(t, uint64(3), frame.Sequence)
}

func TestFrameBuffer_DropPolicyBlock(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   2,
		DropPolicy: DropPolicyBlock,
	})

	// Fill buffer
	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	require.NoError(t, buf.Push(&Frame{Sequence: 2}))

	// Start a goroutine that will push (should block)
	done := make(chan bool)
	go func() {
		err := buf.Push(&Frame{Sequence: 3})
		assert.NoError(t, err)
		done <- true
	}()

	// Give the goroutine time to start blocking
	time.Sleep(10 * time.Millisecond)

	// Pop a frame to unblock
	buf.Pop()

	// Wait for push to complete
	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Push did not unblock")
	}
}

func TestFrameBuffer_Len(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})

	assert.Equal(t, 0, buf.Len())

	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	assert.Equal(t, 1, buf.Len())

	require.NoError(t, buf.Push(&Frame{Sequence: 2}))
	assert.Equal(t, 2, buf.Len())

	buf.Pop()
	assert.Equal(t, 1, buf.Len())
}

func TestFrameBuffer_IsFull(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 2})

	assert.False(t, buf.IsFull())

	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	assert.False(t, buf.IsFull())

	require.NoError(t, buf.Push(&Frame{Sequence: 2}))
	assert.True(t, buf.IsFull())
}

func TestFrameBuffer_IsEmpty(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 2})

	assert.True(t, buf.IsEmpty())

	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	assert.False(t, buf.IsEmpty())

	buf.Pop()
	assert.True(t, buf.IsEmpty())
}

func TestFrameBuffer_Frames(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})

	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	require.NoError(t, buf.Push(&Frame{Sequence: 2}))

	ch := buf.Frames()
	frame := <-ch
	assert.Equal(t, uint64(1), frame.Sequence)
}

func TestFrameBuffer_Stats(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   3,
		DropPolicy: DropPolicyNewest,
	})

	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	require.NoError(t, buf.Push(&Frame{Sequence: 2}))
	require.NoError(t, buf.Push(&Frame{Sequence: 3}))
	_ = buf.Push(&Frame{Sequence: 4}) // This will be dropped (ErrBufferFull)

	stats := buf.Stats()
	assert.Equal(t, 3, stats.Capacity)
	assert.Equal(t, 3, stats.Length)
	assert.Equal(t, uint64(4), stats.TotalFrames)
	assert.Equal(t, uint64(1), stats.DroppedFrames)
	assert.Equal(t, DropPolicyNewest, stats.DropPolicy)
}

func TestFrameBuffer_Clear(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})

	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	require.NoError(t, buf.Push(&Frame{Sequence: 2}))
	require.NoError(t, buf.Push(&Frame{Sequence: 3}))

	assert.Equal(t, 3, buf.Len())

	buf.Clear()
	assert.Equal(t, 0, buf.Len())
	assert.True(t, buf.IsEmpty())
}

func TestFrameBuffer_Close(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{Capacity: 5})

	assert.False(t, buf.IsClosed())

	err := buf.Close()
	require.NoError(t, err)
	assert.True(t, buf.IsClosed())

	// Double close should be safe
	err = buf.Close()
	require.NoError(t, err)
}

func TestFrameBuffer_Concurrent(t *testing.T) {
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   100,
		DropPolicy: DropPolicyNewest,
	})

	var wg sync.WaitGroup
	pushCount := 1000

	// Start multiple pushers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < pushCount/10; j++ {
				_ = buf.Push(&Frame{Sequence: uint64(id*1000 + j)})
			}
		}(i)
	}

	// Start multiple poppers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < pushCount/10; j++ {
				buf.Pop()
				time.Sleep(time.Microsecond)
			}
		}()
	}

	wg.Wait()

	// Verify stats are consistent
	stats := buf.Stats()
	assert.Equal(t, uint64(pushCount), stats.TotalFrames)
}

func TestFrameBufferStats_DropRate(t *testing.T) {
	t.Run("no frames", func(t *testing.T) {
		stats := FrameBufferStats{TotalFrames: 0, DroppedFrames: 0}
		assert.Equal(t, float64(0), stats.DropRate())
	})

	t.Run("no drops", func(t *testing.T) {
		stats := FrameBufferStats{TotalFrames: 100, DroppedFrames: 0}
		assert.Equal(t, float64(0), stats.DropRate())
	})

	t.Run("50% drop rate", func(t *testing.T) {
		stats := FrameBufferStats{TotalFrames: 100, DroppedFrames: 50}
		assert.Equal(t, float64(50), stats.DropRate())
	})

	t.Run("100% drop rate", func(t *testing.T) {
		stats := FrameBufferStats{TotalFrames: 100, DroppedFrames: 100}
		assert.Equal(t, float64(100), stats.DropRate())
	})
}

func TestFrameBufferStats_Utilization(t *testing.T) {
	t.Run("zero capacity", func(t *testing.T) {
		stats := FrameBufferStats{Capacity: 0, Length: 0}
		assert.Equal(t, float64(0), stats.Utilization())
	})

	t.Run("empty buffer", func(t *testing.T) {
		stats := FrameBufferStats{Capacity: 100, Length: 0}
		assert.Equal(t, float64(0), stats.Utilization())
	})

	t.Run("50% utilized", func(t *testing.T) {
		stats := FrameBufferStats{Capacity: 100, Length: 50}
		assert.Equal(t, float64(50), stats.Utilization())
	})

	t.Run("full buffer", func(t *testing.T) {
		stats := FrameBufferStats{Capacity: 100, Length: 100}
		assert.Equal(t, float64(100), stats.Utilization())
	})
}

func TestFrameBuffer_DefaultDropPolicy(t *testing.T) {
	// Test the default case in Push (invalid drop policy)
	buf := NewFrameBuffer(FrameBufferConfig{
		Capacity:   2,
		DropPolicy: DropPolicy(99), // Invalid policy
	})

	require.NoError(t, buf.Push(&Frame{Sequence: 1}))
	require.NoError(t, buf.Push(&Frame{Sequence: 2}))

	// Should behave like DropPolicyNewest
	err := buf.Push(&Frame{Sequence: 3})
	assert.Equal(t, ErrBufferFull, err)
}
