package android

import (
	"context"
	"errors"
	"testing"
)

func TestMockProvider_ListDevices(t *testing.T) {
	provider := NewMockProvider()
	defer func() { _ = provider.Close() }()

	devices := []Device{
		{Serial: "emulator-5554", State: DeviceStateDevice, IsEmulator: true},
		{Serial: "emulator-5556", State: DeviceStateOffline, IsEmulator: true},
	}
	provider.SetDevices(devices)

	t.Run("returns devices", func(t *testing.T) {
		got, err := provider.ListDevices(context.Background())
		if err != nil {
			t.Fatalf("ListDevices() error = %v", err)
		}
		if len(got) != len(devices) {
			t.Errorf("ListDevices() returned %d devices, want %d", len(got), len(devices))
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		provider.Errors.ListDevices = errors.New("test error")
		_, err := provider.ListDevices(context.Background())
		if err == nil {
			t.Error("ListDevices() expected error, got nil")
		}
		provider.Errors.ListDevices = nil
	})

	t.Run("calls OnListDevices callback", func(t *testing.T) {
		called := false
		provider.OnListDevices = func(ctx context.Context) error {
			called = true
			return nil
		}
		_, _ = provider.ListDevices(context.Background())
		if !called {
			t.Error("OnListDevices callback was not called")
		}
		provider.OnListDevices = nil
	})
}

func TestMockProvider_GetDevice(t *testing.T) {
	provider := NewMockProvider()
	defer func() { _ = provider.Close() }()

	device := Device{Serial: "emulator-5554", State: DeviceStateDevice}
	provider.AddDevice(device)

	t.Run("returns device", func(t *testing.T) {
		got, err := provider.GetDevice(context.Background(), "emulator-5554")
		if err != nil {
			t.Fatalf("GetDevice() error = %v", err)
		}
		if got.Serial != device.Serial {
			t.Errorf("GetDevice() serial = %q, want %q", got.Serial, device.Serial)
		}
	})

	t.Run("returns error for unknown device", func(t *testing.T) {
		_, err := provider.GetDevice(context.Background(), "unknown")
		if !errors.Is(err, ErrDeviceNotFound) {
			t.Errorf("GetDevice() error = %v, want %v", err, ErrDeviceNotFound)
		}
	})
}

func TestMockProvider_StartStream(t *testing.T) {
	provider := NewMockProvider()
	defer func() { _ = provider.Close() }()

	device := Device{Serial: "emulator-5554", State: DeviceStateDevice}
	provider.AddDevice(device)

	t.Run("starts stream", func(t *testing.T) {
		opts := StreamOptions{DeviceSerial: "emulator-5554"}
		info, err := provider.StartStream(context.Background(), opts)
		if err != nil {
			t.Fatalf("StartStream() error = %v", err)
		}
		if info.State != StreamStateRunning {
			t.Errorf("StartStream() state = %q, want %q", info.State, StreamStateRunning)
		}
		if info.Device.Serial != device.Serial {
			t.Errorf("StartStream() device serial = %q, want %q", info.Device.Serial, device.Serial)
		}
	})

	t.Run("returns error for duplicate stream", func(t *testing.T) {
		opts := StreamOptions{DeviceSerial: "emulator-5554"}
		_, err := provider.StartStream(context.Background(), opts)
		if !errors.Is(err, ErrStreamAlreadyRunning) {
			t.Errorf("StartStream() error = %v, want %v", err, ErrStreamAlreadyRunning)
		}
	})

	t.Run("returns error for unknown device", func(t *testing.T) {
		opts := StreamOptions{DeviceSerial: "unknown"}
		_, err := provider.StartStream(context.Background(), opts)
		if !errors.Is(err, ErrDeviceNotFound) {
			t.Errorf("StartStream() error = %v, want %v", err, ErrDeviceNotFound)
		}
	})

	t.Run("validates options", func(t *testing.T) {
		opts := StreamOptions{} // Missing device serial
		_, err := provider.StartStream(context.Background(), opts)
		if !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("StartStream() error = %v, want %v", err, ErrInvalidOptions)
		}
	})
}

func TestMockProvider_StopStream(t *testing.T) {
	provider := NewMockProvider()
	defer func() { _ = provider.Close() }()

	device := Device{Serial: "emulator-5554", State: DeviceStateDevice}
	provider.AddDevice(device)

	// Start a stream first
	opts := StreamOptions{DeviceSerial: "emulator-5554"}
	info, _ := provider.StartStream(context.Background(), opts)

	t.Run("stops stream", func(t *testing.T) {
		err := provider.StopStream(context.Background(), info.ID)
		if err != nil {
			t.Fatalf("StopStream() error = %v", err)
		}

		// Verify stream is removed
		_, err = provider.GetStream(context.Background(), info.ID)
		if !errors.Is(err, ErrStreamNotFound) {
			t.Errorf("GetStream() error = %v, want %v", err, ErrStreamNotFound)
		}
	})

	t.Run("returns error for unknown stream", func(t *testing.T) {
		err := provider.StopStream(context.Background(), "unknown")
		if !errors.Is(err, ErrStreamNotFound) {
			t.Errorf("StopStream() error = %v, want %v", err, ErrStreamNotFound)
		}
	})
}

func TestMockProvider_GetStream(t *testing.T) {
	provider := NewMockProvider()
	defer func() { _ = provider.Close() }()

	device := Device{Serial: "emulator-5554", State: DeviceStateDevice}
	provider.AddDevice(device)

	// Start a stream
	opts := StreamOptions{DeviceSerial: "emulator-5554"}
	info, _ := provider.StartStream(context.Background(), opts)

	t.Run("returns stream", func(t *testing.T) {
		got, err := provider.GetStream(context.Background(), info.ID)
		if err != nil {
			t.Fatalf("GetStream() error = %v", err)
		}
		if got.ID != info.ID {
			t.Errorf("GetStream() ID = %q, want %q", got.ID, info.ID)
		}
	})

	t.Run("returns error for unknown stream", func(t *testing.T) {
		_, err := provider.GetStream(context.Background(), "unknown")
		if !errors.Is(err, ErrStreamNotFound) {
			t.Errorf("GetStream() error = %v, want %v", err, ErrStreamNotFound)
		}
	})
}

func TestMockProvider_ListStreams(t *testing.T) {
	provider := NewMockProvider()
	defer func() { _ = provider.Close() }()

	device := Device{Serial: "emulator-5554", State: DeviceStateDevice}
	provider.AddDevice(device)

	t.Run("returns empty list initially", func(t *testing.T) {
		streams, err := provider.ListStreams(context.Background())
		if err != nil {
			t.Fatalf("ListStreams() error = %v", err)
		}
		if len(streams) != 0 {
			t.Errorf("ListStreams() returned %d streams, want 0", len(streams))
		}
	})

	t.Run("returns active streams", func(t *testing.T) {
		opts := StreamOptions{DeviceSerial: "emulator-5554"}
		_, _ = provider.StartStream(context.Background(), opts)

		streams, err := provider.ListStreams(context.Background())
		if err != nil {
			t.Fatalf("ListStreams() error = %v", err)
		}
		if len(streams) != 1 {
			t.Errorf("ListStreams() returned %d streams, want 1", len(streams))
		}
	})
}

func TestMockProvider_SendInput(t *testing.T) {
	provider := NewMockProvider()
	defer func() { _ = provider.Close() }()

	device := Device{Serial: "emulator-5554", State: DeviceStateDevice}
	provider.AddDevice(device)

	t.Run("sends tap", func(t *testing.T) {
		event := InputEvent{Type: InputTypeTap, X: 100, Y: 200}
		err := provider.SendInput(context.Background(), "emulator-5554", event)
		if err != nil {
			t.Fatalf("SendInput() error = %v", err)
		}
	})

	t.Run("validates input", func(t *testing.T) {
		event := InputEvent{Type: InputTypeTap, X: -1, Y: 200}
		err := provider.SendInput(context.Background(), "emulator-5554", event)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("SendInput() error = %v, want %v", err, ErrInvalidInput)
		}
	})

	t.Run("returns error for unknown device", func(t *testing.T) {
		event := InputEvent{Type: InputTypeTap, X: 100, Y: 200}
		err := provider.SendInput(context.Background(), "unknown", event)
		if !errors.Is(err, ErrDeviceNotFound) {
			t.Errorf("SendInput() error = %v, want %v", err, ErrDeviceNotFound)
		}
	})

	t.Run("calls OnSendInput callback", func(t *testing.T) {
		var capturedEvent InputEvent
		provider.OnSendInput = func(ctx context.Context, serial string, event InputEvent) error {
			capturedEvent = event
			return nil
		}

		event := InputEvent{Type: InputTypeKey, KeyCode: KeyCodeHome}
		_ = provider.SendInput(context.Background(), "emulator-5554", event)

		if capturedEvent.Type != event.Type {
			t.Errorf("OnSendInput event type = %q, want %q", capturedEvent.Type, event.Type)
		}
		provider.OnSendInput = nil
	})
}

func TestMockProvider_Close(t *testing.T) {
	provider := NewMockProvider()

	device := Device{Serial: "emulator-5554", State: DeviceStateDevice}
	provider.AddDevice(device)

	// Start a stream
	opts := StreamOptions{DeviceSerial: "emulator-5554"}
	_, _ = provider.StartStream(context.Background(), opts)

	t.Run("closes provider", func(t *testing.T) {
		err := provider.Close()
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		if !provider.IsClosed() {
			t.Error("IsClosed() = false, want true")
		}
	})

	t.Run("returns error after close", func(t *testing.T) {
		_, err := provider.ListDevices(context.Background())
		if !errors.Is(err, ErrProviderClosed) {
			t.Errorf("ListDevices() error = %v, want %v", err, ErrProviderClosed)
		}
	})
}

func TestMockProvider_Reset(t *testing.T) {
	provider := NewMockProvider()

	device := Device{Serial: "emulator-5554", State: DeviceStateDevice}
	provider.AddDevice(device)
	provider.Errors.ListDevices = errors.New("test")

	provider.Reset()

	devices, err := provider.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("ListDevices() after reset error = %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("ListDevices() after reset returned %d devices, want 0", len(devices))
	}
}

func TestMockVideoSink(t *testing.T) {
	sink := NewMockVideoSink()

	t.Run("records video data", func(t *testing.T) {
		data := []byte{0x00, 0x00, 0x00, 0x01, 0x67}
		err := sink.OnVideoData(data)
		if err != nil {
			t.Fatalf("OnVideoData() error = %v", err)
		}
		if len(sink.VideoDataCalls) != 1 {
			t.Errorf("VideoDataCalls = %d, want 1", len(sink.VideoDataCalls))
		}
	})

	t.Run("records video config", func(t *testing.T) {
		err := sink.OnVideoConfig(1920, 1080, "h264", []byte{0x67, 0x68})
		if err != nil {
			t.Fatalf("OnVideoConfig() error = %v", err)
		}
		if len(sink.VideoConfigCalls) != 1 {
			t.Errorf("VideoConfigCalls = %d, want 1", len(sink.VideoConfigCalls))
		}
		call := sink.VideoConfigCalls[0]
		if call.Width != 1920 || call.Height != 1080 {
			t.Errorf("VideoConfig dimensions = %dx%d, want 1920x1080", call.Width, call.Height)
		}
	})

	t.Run("records audio data", func(t *testing.T) {
		data := []byte{0x00, 0x01, 0x02}
		err := sink.OnAudioData(data)
		if err != nil {
			t.Fatalf("OnAudioData() error = %v", err)
		}
		if len(sink.AudioDataCalls) != 1 {
			t.Errorf("AudioDataCalls = %d, want 1", len(sink.AudioDataCalls))
		}
	})

	t.Run("records audio config", func(t *testing.T) {
		err := sink.OnAudioConfig(48000, 2, "opus", nil)
		if err != nil {
			t.Fatalf("OnAudioConfig() error = %v", err)
		}
		if len(sink.AudioConfigCalls) != 1 {
			t.Errorf("AudioConfigCalls = %d, want 1", len(sink.AudioConfigCalls))
		}
	})

	t.Run("records errors", func(t *testing.T) {
		testErr := errors.New("test error")
		sink.OnError(testErr)
		if len(sink.ErrorCalls) != 1 {
			t.Errorf("ErrorCalls = %d, want 1", len(sink.ErrorCalls))
		}
	})

	t.Run("records close", func(t *testing.T) {
		sink.OnClose()
		if !sink.CloseCalled {
			t.Error("CloseCalled = false, want true")
		}
	})

	t.Run("reset clears all", func(t *testing.T) {
		sink.Reset()
		if len(sink.VideoDataCalls) != 0 {
			t.Error("VideoDataCalls not cleared")
		}
		if sink.CloseCalled {
			t.Error("CloseCalled not cleared")
		}
	})
}

func TestMockInputHandler(t *testing.T) {
	handler := NewMockInputHandler()
	ctx := context.Background()

	t.Run("records tap", func(t *testing.T) {
		err := handler.HandleTap(ctx, 100, 200)
		if err != nil {
			t.Fatalf("HandleTap() error = %v", err)
		}
		if len(handler.TapCalls) != 1 {
			t.Errorf("TapCalls = %d, want 1", len(handler.TapCalls))
		}
		if handler.TapCalls[0].X != 100 || handler.TapCalls[0].Y != 200 {
			t.Errorf("TapCalls[0] = %+v, want {X:100 Y:200}", handler.TapCalls[0])
		}
	})

	t.Run("records swipe", func(t *testing.T) {
		err := handler.HandleSwipe(ctx, 100, 200, 300, 400, 150)
		if err != nil {
			t.Fatalf("HandleSwipe() error = %v", err)
		}
		if len(handler.SwipeCalls) != 1 {
			t.Errorf("SwipeCalls = %d, want 1", len(handler.SwipeCalls))
		}
	})

	t.Run("records long press", func(t *testing.T) {
		err := handler.HandleLongPress(ctx, 100, 200, 500)
		if err != nil {
			t.Fatalf("HandleLongPress() error = %v", err)
		}
		if len(handler.LongPressCalls) != 1 {
			t.Errorf("LongPressCalls = %d, want 1", len(handler.LongPressCalls))
		}
	})

	t.Run("records text", func(t *testing.T) {
		err := handler.HandleText(ctx, "hello")
		if err != nil {
			t.Fatalf("HandleText() error = %v", err)
		}
		if len(handler.TextCalls) != 1 || handler.TextCalls[0] != "hello" {
			t.Errorf("TextCalls = %v, want [hello]", handler.TextCalls)
		}
	})

	t.Run("records key", func(t *testing.T) {
		err := handler.HandleKey(ctx, KeyCodeEnter)
		if err != nil {
			t.Fatalf("HandleKey() error = %v", err)
		}
		if len(handler.KeyCalls) != 1 || handler.KeyCalls[0] != KeyCodeEnter {
			t.Errorf("KeyCalls = %v, want [%d]", handler.KeyCalls, KeyCodeEnter)
		}
	})

	t.Run("records back", func(t *testing.T) {
		err := handler.HandleBack(ctx)
		if err != nil {
			t.Fatalf("HandleBack() error = %v", err)
		}
		if handler.BackCalls != 1 {
			t.Errorf("BackCalls = %d, want 1", handler.BackCalls)
		}
	})

	t.Run("records home", func(t *testing.T) {
		err := handler.HandleHome(ctx)
		if err != nil {
			t.Fatalf("HandleHome() error = %v", err)
		}
		if handler.HomeCalls != 1 {
			t.Errorf("HomeCalls = %d, want 1", handler.HomeCalls)
		}
	})

	t.Run("records recent", func(t *testing.T) {
		err := handler.HandleRecent(ctx)
		if err != nil {
			t.Fatalf("HandleRecent() error = %v", err)
		}
		if handler.RecentCalls != 1 {
			t.Errorf("RecentCalls = %d, want 1", handler.RecentCalls)
		}
	})

	t.Run("sets display size", func(t *testing.T) {
		handler.SetDisplaySize(1920, 1080)
		if handler.DisplayWidth != 1920 || handler.DisplayHeight != 1080 {
			t.Errorf("DisplaySize = %dx%d, want 1920x1080", handler.DisplayWidth, handler.DisplayHeight)
		}
	})

	t.Run("returns configured error", func(t *testing.T) {
		handler.Error = errors.New("test error")
		err := handler.HandleTap(ctx, 0, 0)
		if err == nil {
			t.Error("HandleTap() expected error, got nil")
		}
		handler.Error = nil
	})

	t.Run("reset clears all", func(t *testing.T) {
		handler.Reset()
		if len(handler.TapCalls) != 0 {
			t.Error("TapCalls not cleared")
		}
		if handler.BackCalls != 0 {
			t.Error("BackCalls not cleared")
		}
	})
}
