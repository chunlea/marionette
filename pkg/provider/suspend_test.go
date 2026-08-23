package provider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "duration string", input: `"90s"`, want: 90 * time.Second},
		{name: "hours", input: `"24h"`, want: 24 * time.Hour},
		{name: "bare number is seconds", input: `120`, want: 120 * time.Second},
		{name: "invalid string", input: `"nope"`, wantErr: true},
		{name: "invalid type", input: `true`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := json.Unmarshal([]byte(tt.input), &d)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, d.Duration())
		})
	}
}

func TestDuration_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(Duration(5 * time.Minute))
	require.NoError(t, err)
	assert.JSONEq(t, `"5m0s"`, string(data))
}

func TestParseSuspendConfig(t *testing.T) {
	defaults := SuspendConfig{
		Strategy:    SuspendStrategyPause,
		MinDuration: 60 * time.Second,
		MaxDuration: 24 * time.Hour,
		Fallback:    SuspendStrategyTerminate,
	}

	t.Run("empty input uses defaults", func(t *testing.T) {
		cfg, err := ParseSuspendConfig(nil, defaults)
		require.NoError(t, err)
		assert.Equal(t, defaults, *cfg)
	})

	t.Run("explicit values win", func(t *testing.T) {
		cfg, err := ParseSuspendConfig(json.RawMessage(`{
			"strategy": "terminate",
			"min_duration": "30s",
			"max_duration": 3600,
			"fallback": "terminate_preserve_storage"
		}`), defaults)
		require.NoError(t, err)
		assert.Equal(t, SuspendStrategyTerminate, cfg.Strategy)
		assert.Equal(t, 30*time.Second, cfg.MinDuration)
		assert.Equal(t, time.Hour, cfg.MaxDuration)
		assert.Equal(t, SuspendStrategyTerminatePreserveStorage, cfg.Fallback)
	})

	t.Run("malformed json errors", func(t *testing.T) {
		_, err := ParseSuspendConfig(json.RawMessage(`{`), defaults)
		require.Error(t, err)
	})
}

func newTestDispatcher(t *testing.T, calls *[]SuspendStrategy, failing map[SuspendStrategy]error) SuspendDispatcher {
	t.Helper()

	handler := func(s SuspendStrategy) SuspendFunc {
		return func(_ context.Context, _ string) error {
			*calls = append(*calls, s)
			return failing[s]
		}
	}

	return SuspendDispatcher{
		Provider: "test",
		Config: SuspendConfig{
			Strategy: SuspendStrategyPause,
			Fallback: SuspendStrategyTerminate,
		},
		Handlers: map[SuspendStrategy]SuspendFunc{
			SuspendStrategyPause:     handler(SuspendStrategyPause),
			SuspendStrategyTerminate: handler(SuspendStrategyTerminate),
		},
	}
}

func TestSuspendDispatcher_Strategies(t *testing.T) {
	var calls []SuspendStrategy
	d := newTestDispatcher(t, &calls, nil)

	// Canonical order, and only what has a handler.
	assert.Equal(t, []SuspendStrategy{SuspendStrategyPause, SuspendStrategyTerminate}, d.Strategies())
	assert.True(t, d.Supports(SuspendStrategyPause))
	assert.False(t, d.Supports(SuspendStrategySnapshot))
}

func TestSuspendDispatcher_UsesConfiguredStrategyByDefault(t *testing.T) {
	var calls []SuspendStrategy
	d := newTestDispatcher(t, &calls, nil)

	result, err := d.Suspend(context.Background(), "run_1", SuspendOptions{})
	require.NoError(t, err)
	assert.Equal(t, SuspendStrategyPause, result.Strategy)
	assert.Equal(t, []SuspendStrategy{SuspendStrategyPause}, calls)
}

func TestSuspendDispatcher_UnsupportedStrategyIsNotSubstituted(t *testing.T) {
	var calls []SuspendStrategy
	d := newTestDispatcher(t, &calls, nil)

	_, err := d.Suspend(context.Background(), "run_1", SuspendOptions{
		Strategy: SuspendStrategySnapshot,
	})

	var notSupported *ErrStrategyNotSupported
	require.True(t, errors.As(err, &notSupported))
	assert.Equal(t, SuspendStrategySnapshot, notSupported.Strategy)
	assert.Empty(t, calls, "an unsupported strategy must not quietly run the fallback")
}

func TestSuspendDispatcher_FallsBackOnFailure(t *testing.T) {
	var calls []SuspendStrategy
	d := newTestDispatcher(t, &calls, map[SuspendStrategy]error{
		SuspendStrategyPause: errors.New("pause broke"),
	})

	result, err := d.Suspend(context.Background(), "run_1", SuspendOptions{})
	require.NoError(t, err)
	assert.Equal(t, SuspendStrategyTerminate, result.Strategy)
	assert.Equal(t, []SuspendStrategy{SuspendStrategyPause, SuspendStrategyTerminate}, calls)
}

// TestSuspendDispatcher_KeepsOriginalErrorWhenFallbackFails is the regression
// test for the per-provider copies, which recursed into the fallback and threw
// the original failure away.
func TestSuspendDispatcher_KeepsOriginalErrorWhenFallbackFails(t *testing.T) {
	var calls []SuspendStrategy
	d := newTestDispatcher(t, &calls, map[SuspendStrategy]error{
		SuspendStrategyPause:     errors.New("pause broke"),
		SuspendStrategyTerminate: errors.New("terminate broke"),
	})

	_, err := d.Suspend(context.Background(), "run_1", SuspendOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pause broke", "the original failure must survive")
	assert.Contains(t, err.Error(), "terminate broke")
}

// TestSuspendDispatcher_WorkspaceSyncedIsHonest guards against reporting a
// workspace sync that no provider actually performs (decision D3).
func TestSuspendDispatcher_WorkspaceSyncedIsHonest(t *testing.T) {
	var calls []SuspendStrategy
	d := newTestDispatcher(t, &calls, nil)

	result, err := d.Suspend(context.Background(), "run_1", SuspendOptions{
		SyncWorkspace: true,
	})
	require.NoError(t, err)
	assert.False(t, result.WorkspaceSynced, "no CAS sync exists, so this must not claim one")
}
