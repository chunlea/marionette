package core

import (
	"context"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
)

func TestFrameHub_RegisterUnregisterStream(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"
	runnerID := "runner-1"
	sessionID := "sess_test123"

	// Register a stream
	sendInput := func(event *pb.BrowserInputEvent) error { return nil }
	sendControl := func(msg *pb.ServerBrowserMessage) error { return nil }

	inputCh, err := hub.RegisterStream(tunnelID, runnerID, sessionID, sendInput, sendControl)
	if err != nil {
		t.Fatalf("RegisterStream() error = %v", err)
	}
	if inputCh == nil {
		t.Fatal("RegisterStream() returned nil input channel")
	}

	// Verify stream is registered
	stream := hub.GetStream(tunnelID)
	if stream == nil {
		t.Fatal("GetStream() returned nil after registration")
	}
	if stream.TunnelID != tunnelID {
		t.Errorf("TunnelID = %v, want %v", stream.TunnelID, tunnelID)
	}
	if stream.RunnerID != runnerID {
		t.Errorf("RunnerID = %v, want %v", stream.RunnerID, runnerID)
	}
	if !stream.Connected {
		t.Error("stream.Connected = false, want true")
	}

	// Verify in active tunnels list
	activeTunnels := hub.ListActiveTunnels()
	found := false
	for _, tid := range activeTunnels {
		if tid == tunnelID {
			found = true
			break
		}
	}
	if !found {
		t.Error("tunnel not in ListActiveTunnels()")
	}

	// Unregister stream
	hub.UnregisterStream(tunnelID)

	// Verify stream is unregistered
	stream = hub.GetStream(tunnelID)
	if stream != nil {
		t.Error("GetStream() should return nil after unregister")
	}
}

func TestFrameHub_SubscribeUnsubscribe(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"

	// Create subscriber
	subscriber := &FrameSubscriber{
		ID:        "sub-1",
		TunnelID:  tunnelID,
		FrameCh:   make(chan *pb.BrowserFrame, 10),
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
	}

	// Subscribe
	hub.Subscribe(subscriber)

	if hub.GetSubscriberCount(tunnelID) != 1 {
		t.Errorf("GetSubscriberCount() = %d, want 1", hub.GetSubscriberCount(tunnelID))
	}

	// Add another subscriber
	subscriber2 := &FrameSubscriber{
		ID:        "sub-2",
		TunnelID:  tunnelID,
		FrameCh:   make(chan *pb.BrowserFrame, 10),
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
	}
	hub.Subscribe(subscriber2)

	if hub.GetSubscriberCount(tunnelID) != 2 {
		t.Errorf("GetSubscriberCount() = %d, want 2", hub.GetSubscriberCount(tunnelID))
	}

	// Unsubscribe first subscriber
	hub.Unsubscribe(subscriber)

	if hub.GetSubscriberCount(tunnelID) != 1 {
		t.Errorf("GetSubscriberCount() after unsubscribe = %d, want 1", hub.GetSubscriberCount(tunnelID))
	}

	// Unsubscribe second subscriber
	hub.Unsubscribe(subscriber2)

	if hub.GetSubscriberCount(tunnelID) != 0 {
		t.Errorf("GetSubscriberCount() after all unsubscribe = %d, want 0", hub.GetSubscriberCount(tunnelID))
	}
}

func TestFrameHub_BroadcastFrame(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"

	// Register stream first
	_, err := hub.RegisterStream(tunnelID, "runner-1", "sess_test123",
		func(*pb.BrowserInputEvent) error { return nil },
		func(*pb.ServerBrowserMessage) error { return nil },
	)
	if err != nil {
		t.Fatalf("RegisterStream() error = %v", err)
	}

	// Create subscribers
	sub1 := &FrameSubscriber{
		ID:        "sub-1",
		TunnelID:  tunnelID,
		FrameCh:   make(chan *pb.BrowserFrame, 10),
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
	}
	sub2 := &FrameSubscriber{
		ID:        "sub-2",
		TunnelID:  tunnelID,
		FrameCh:   make(chan *pb.BrowserFrame, 10),
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
	}

	hub.Subscribe(sub1)
	hub.Subscribe(sub2)

	// Broadcast a frame
	frame := &pb.BrowserFrame{
		Sequence: 1,
		Data:     []byte("test-frame"),
		Format:   "jpeg",
		Width:    1920,
		Height:   1080,
	}

	hub.BroadcastFrame(tunnelID, frame)

	// Both subscribers should receive the frame
	select {
	case received := <-sub1.FrameCh:
		if received.Sequence != frame.Sequence {
			t.Errorf("sub1 received frame.Sequence = %d, want %d", received.Sequence, frame.Sequence)
		}
	default:
		t.Error("sub1 did not receive frame")
	}

	select {
	case received := <-sub2.FrameCh:
		if received.Sequence != frame.Sequence {
			t.Errorf("sub2 received frame.Sequence = %d, want %d", received.Sequence, frame.Sequence)
		}
	default:
		t.Error("sub2 did not receive frame")
	}

	// Verify stats
	if sub1.FramesDelivered != 1 {
		t.Errorf("sub1.FramesDelivered = %d, want 1", sub1.FramesDelivered)
	}
	if sub2.FramesDelivered != 1 {
		t.Errorf("sub2.FramesDelivered = %d, want 1", sub2.FramesDelivered)
	}
}

func TestFrameHub_BroadcastFrame_DroppedWhenFull(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"

	// Register stream
	_, err := hub.RegisterStream(tunnelID, "runner-1", "sess_test123",
		func(*pb.BrowserInputEvent) error { return nil },
		func(*pb.ServerBrowserMessage) error { return nil },
	)
	if err != nil {
		t.Fatalf("RegisterStream() error = %v", err)
	}

	// Create subscriber with small buffer
	sub := &FrameSubscriber{
		ID:        "sub-1",
		TunnelID:  tunnelID,
		FrameCh:   make(chan *pb.BrowserFrame, 1), // Small buffer
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
	}

	hub.Subscribe(sub)

	// Broadcast multiple frames without consuming
	for i := 0; i < 5; i++ {
		hub.BroadcastFrame(tunnelID, &pb.BrowserFrame{Sequence: uint64(i)})
	}

	// First frame should be in channel, rest should be dropped
	if sub.FramesDelivered != 1 {
		t.Errorf("FramesDelivered = %d, want 1", sub.FramesDelivered)
	}
	if sub.FramesDropped != 4 {
		t.Errorf("FramesDropped = %d, want 4", sub.FramesDropped)
	}
}

func TestFrameHub_SendInput(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"

	// Test without stream registered
	err := hub.SendInput(context.Background(), tunnelID, &pb.BrowserInputEvent{})
	if err != ErrStreamNotConnected {
		t.Errorf("SendInput() without stream error = %v, want ErrStreamNotConnected", err)
	}

	// Register stream with input handler
	var receivedInput *pb.BrowserInputEvent
	sendInput := func(event *pb.BrowserInputEvent) error {
		receivedInput = event
		return nil
	}

	_, err = hub.RegisterStream(tunnelID, "runner-1", "sess_test123",
		sendInput,
		func(*pb.ServerBrowserMessage) error { return nil },
	)
	if err != nil {
		t.Fatalf("RegisterStream() error = %v", err)
	}

	// Send input
	inputEvent := &pb.BrowserInputEvent{
		Type: "mouseDown",
		Event: &pb.BrowserInputEvent_Mouse{
			Mouse: &pb.BrowserMouseEvent{
				X:      100,
				Y:      200,
				Button: "left",
			},
		},
	}

	err = hub.SendInput(context.Background(), tunnelID, inputEvent)
	if err != nil {
		t.Errorf("SendInput() error = %v", err)
	}

	if receivedInput == nil {
		t.Fatal("input handler was not called")
	}
	if receivedInput.Type != inputEvent.Type {
		t.Errorf("received event type = %v, want %v", receivedInput.Type, inputEvent.Type)
	}
}

func TestFrameHub_SendControl(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"

	// Test without stream registered
	err := hub.SendControl(context.Background(), tunnelID, &pb.ServerBrowserMessage{})
	if err != ErrStreamNotConnected {
		t.Errorf("SendControl() without stream error = %v, want ErrStreamNotConnected", err)
	}

	// Register stream with control handler
	var receivedControl *pb.ServerBrowserMessage
	sendControl := func(msg *pb.ServerBrowserMessage) error {
		receivedControl = msg
		return nil
	}

	_, err = hub.RegisterStream(tunnelID, "runner-1", "sess_test123",
		func(*pb.BrowserInputEvent) error { return nil },
		sendControl,
	)
	if err != nil {
		t.Fatalf("RegisterStream() error = %v", err)
	}

	// Send control
	controlMsg := &pb.ServerBrowserMessage{
		Payload: &pb.ServerBrowserMessage_Control{
			Control: &pb.BrowserStreamControl{
				Command: &pb.BrowserStreamControl_Pause{Pause: true},
			},
		},
	}

	err = hub.SendControl(context.Background(), tunnelID, controlMsg)
	if err != nil {
		t.Errorf("SendControl() error = %v", err)
	}

	if receivedControl == nil {
		t.Fatal("control handler was not called")
	}
}

func TestFrameHub_QueueInput(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"

	// Test without stream registered
	if hub.QueueInput(tunnelID, &pb.BrowserInputEvent{}) {
		t.Error("QueueInput() should return false without stream")
	}

	// Register stream
	inputCh, err := hub.RegisterStream(tunnelID, "runner-1", "sess_test123",
		func(*pb.BrowserInputEvent) error { return nil },
		func(*pb.ServerBrowserMessage) error { return nil },
	)
	if err != nil {
		t.Fatalf("RegisterStream() error = %v", err)
	}

	// Queue input
	inputEvent := &pb.BrowserInputEvent{
		Type: "keyDown",
	}

	if !hub.QueueInput(tunnelID, inputEvent) {
		t.Error("QueueInput() should return true")
	}

	// Read from channel
	select {
	case received := <-inputCh:
		if received.Type != inputEvent.Type {
			t.Errorf("received event type = %v, want %v", received.Type, inputEvent.Type)
		}
	default:
		t.Error("input event not in channel")
	}
}

func TestFrameHub_GetStats(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"

	// Stats for non-existent tunnel
	stats := hub.GetStats(tunnelID)
	if stats.StreamConnected {
		t.Error("StreamConnected = true for non-existent tunnel")
	}

	// Register stream
	_, err := hub.RegisterStream(tunnelID, "runner-1", "sess_test123",
		func(*pb.BrowserInputEvent) error { return nil },
		func(*pb.ServerBrowserMessage) error { return nil },
	)
	if err != nil {
		t.Fatalf("RegisterStream() error = %v", err)
	}

	// Add subscriber
	sub := &FrameSubscriber{
		ID:        "sub-1",
		TunnelID:  tunnelID,
		FrameCh:   make(chan *pb.BrowserFrame, 10),
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
	}
	hub.Subscribe(sub)

	// Broadcast some frames
	for i := 0; i < 3; i++ {
		hub.BroadcastFrame(tunnelID, &pb.BrowserFrame{Sequence: uint64(i)})
	}

	// Get stats
	stats = hub.GetStats(tunnelID)
	if !stats.StreamConnected {
		t.Error("StreamConnected = false")
	}
	if stats.FramesReceived != 3 {
		t.Errorf("FramesReceived = %d, want 3", stats.FramesReceived)
	}
	if stats.SubscriberCount != 1 {
		t.Errorf("SubscriberCount = %d, want 1", stats.SubscriberCount)
	}
	if stats.TotalFramesDelivered != 3 {
		t.Errorf("TotalFramesDelivered = %d, want 3", stats.TotalFramesDelivered)
	}
}

func TestFrameHub_ReplaceStream(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"

	// Register first stream
	_, err := hub.RegisterStream(tunnelID, "runner-1", "sess_test123",
		func(*pb.BrowserInputEvent) error { return nil },
		func(*pb.ServerBrowserMessage) error { return nil },
	)
	if err != nil {
		t.Fatalf("RegisterStream() error = %v", err)
	}

	// Register second stream (should replace)
	_, err = hub.RegisterStream(tunnelID, "runner-2", "sess_test123",
		func(*pb.BrowserInputEvent) error { return nil },
		func(*pb.ServerBrowserMessage) error { return nil },
	)
	if err != nil {
		t.Fatalf("RegisterStream() second time error = %v", err)
	}

	// Verify new runner ID
	stream := hub.GetStream(tunnelID)
	if stream.RunnerID != "runner-2" {
		t.Errorf("RunnerID = %v, want runner-2", stream.RunnerID)
	}
}

func TestFrameHub_Concurrent(t *testing.T) {
	hub := NewFrameHub(zap.NewNop())
	defer hub.Close()

	tunnelID := "tun_test123"

	// Register stream
	_, err := hub.RegisterStream(tunnelID, "runner-1", "sess_test123",
		func(*pb.BrowserInputEvent) error { return nil },
		func(*pb.ServerBrowserMessage) error { return nil },
	)
	if err != nil {
		t.Fatalf("RegisterStream() error = %v", err)
	}

	// Create multiple subscribers
	numSubscribers := 5
	subscribers := make([]*FrameSubscriber, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		subscribers[i] = &FrameSubscriber{
			ID:        "sub-" + string(rune('0'+i)),
			TunnelID:  tunnelID,
			FrameCh:   make(chan *pb.BrowserFrame, 100),
			Done:      make(chan struct{}),
			CreatedAt: time.Now(),
		}
		hub.Subscribe(subscribers[i])
	}

	// Concurrently broadcast frames and consume them
	var wg sync.WaitGroup
	numFrames := 50

	// Broadcaster
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numFrames; i++ {
			hub.BroadcastFrame(tunnelID, &pb.BrowserFrame{Sequence: uint64(i)})
		}
	}()

	// Consumers
	for _, sub := range subscribers {
		wg.Add(1)
		go func(s *FrameSubscriber) {
			defer wg.Done()
			count := 0
			for count < numFrames {
				select {
				case <-s.FrameCh:
					count++
				case <-time.After(100 * time.Millisecond):
					return
				}
			}
		}(sub)
	}

	wg.Wait()

	// Verify stats
	stats := hub.GetStats(tunnelID)
	if stats.FramesReceived != uint64(numFrames) {
		t.Errorf("FramesReceived = %d, want %d", stats.FramesReceived, numFrames)
	}
}
