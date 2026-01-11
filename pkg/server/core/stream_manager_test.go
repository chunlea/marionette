package core

import (
	"testing"

	"go.uber.org/zap"
)

func TestDefaultStreamManagerConfig(t *testing.T) {
	config := DefaultStreamManagerConfig()

	if config.WebSocketReadBufferSize != 1024 {
		t.Errorf("expected WebSocketReadBufferSize 1024, got %d", config.WebSocketReadBufferSize)
	}
	if config.WebSocketWriteBufferSize != 1024 {
		t.Errorf("expected WebSocketWriteBufferSize 1024, got %d", config.WebSocketWriteBufferSize)
	}
	if len(config.AllowedOrigins) != 1 || config.AllowedOrigins[0] != "*" {
		t.Errorf("expected AllowedOrigins [*], got %v", config.AllowedOrigins)
	}
}

func TestNewStreamManager(t *testing.T) {
	config := DefaultStreamManagerConfig()
	logger := zap.NewNop()

	// Create a mock store
	store := newTestStore()

	mgr, err := NewStreamManager(config, store, logger)
	if err != nil {
		t.Fatalf("failed to create stream manager: %v", err)
	}

	if mgr == nil {
		t.Fatal("expected non-nil stream manager")
	}

	if mgr.GetSFU() == nil {
		t.Error("expected non-nil SFU")
	}

	if mgr.GetSignalingHandler() == nil {
		t.Error("expected non-nil signaling handler")
	}
}

func TestNewStreamManagerNilLogger(t *testing.T) {
	config := DefaultStreamManagerConfig()
	store := newTestStore()

	// Should not panic with nil logger
	mgr, err := NewStreamManager(config, store, nil)
	if err != nil {
		t.Fatalf("failed to create stream manager: %v", err)
	}

	if mgr == nil {
		t.Fatal("expected non-nil stream manager")
	}
}
