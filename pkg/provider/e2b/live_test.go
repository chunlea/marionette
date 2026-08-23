package e2b

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// Live tests run against api.e2b.dev with a real key and create real, billed
// sandboxes. They are opt-in: without E2B_API_KEY they skip.
//
// NEVER hardcode a key here or anywhere else in the tree. Run them with:
//
//	E2B_API_KEY=... go test ./pkg/provider/e2b/ -run TestE2BLive -v
//
// Every test kills what it created, including on failure, because an E2B
// sandbox left behind bills until its timeout expires - and a paused one, which
// is what these tests deliberately produce, is exactly the thing that used to
// become impossible to find.

const liveSkipMessage = "E2B_API_KEY is not set: skipping the live E2B tests. " +
	"They create real, billed sandboxes against api.e2b.dev."

// liveProvider builds a provider against the real API, or skips.
func liveProvider(t *testing.T) *Provider {
	t.Helper()

	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		t.Skip(liveSkipMessage)
	}

	cfg := &store.ProviderConfig{
		Name:     "live-e2b",
		Provider: "e2b",
		Config: json.RawMessage(`{
			"api_url": "https://api.e2b.dev",
			"api_key": "` + apiKey + `",
			"template": "base",
			"timeout_seconds": 120
		}`),
		SuspendConfig: json.RawMessage(`{"strategy": "pause"}`),
	}

	p, err := New(cfg)
	require.NoError(t, err)
	return p
}

// killSandbox removes a sandbox regardless of test outcome. A failure to clean
// up is reported rather than swallowed: the account is left dirty and someone
// has to know.
func killSandbox(t *testing.T, p *Provider, runnerID, instanceID string) {
	t.Helper()
	err := p.Destroy(context.Background(), runnerID, provider.DestroyOptions{
		ProviderInstanceID: instanceID,
	})
	if err != nil && !IsNotFoundError(err) {
		t.Errorf("failed to clean up sandbox %s: %v", instanceID, err)
	}
}

// TestE2BLive_Lifecycle walks create -> pause -> resume -> kill against the
// real API.
func TestE2BLive_Lifecycle(t *testing.T) {
	p := liveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	runnerID := "run_live_lifecycle"

	instance, err := p.Spawn(ctx, provider.SpawnOptions{
		RunnerID: runnerID,
		Name:     "marionette-live-test",
	})
	require.NoError(t, err)
	require.NotEmpty(t, instance.ProviderID)
	t.Cleanup(func() { killSandbox(t, p, runnerID, instance.ProviderID) })

	t.Logf("created sandbox %s", instance.ProviderID)

	result, err := p.Suspend(ctx, runnerID, provider.SuspendOptions{
		Strategy:           provider.SuspendStrategyPause,
		ProviderInstanceID: instance.ProviderID,
	})
	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyPause, result.Strategy)

	// The live API does not list paused sandboxes. This is the property the
	// whole persisted-id change rests on, so assert it rather than trust it.
	listed, err := p.List(ctx)
	require.NoError(t, err)
	for _, item := range listed {
		assert.NotEqual(t, instance.ProviderID, item.ProviderID,
			"a paused sandbox must not appear in the list; if this fires, E2B changed and the orphan risk is gone")
	}

	resumed, err := p.Resume(ctx, "sess_live", provider.ResumeOptions{
		RunnerID:           runnerID,
		ProviderInstanceID: instance.ProviderID,
	})
	require.NoError(t, err)
	assert.Equal(t, instance.ProviderID, resumed.ProviderID)

	require.NoError(t, p.Destroy(ctx, runnerID, provider.DestroyOptions{
		ProviderInstanceID: instance.ProviderID,
	}))
}

// TestE2BLive_RestartOrphan is the live version of the regression: pause a real
// sandbox, drop the provider entirely, and resume it from nothing but the
// persisted id.
func TestE2BLive_RestartOrphan(t *testing.T) {
	first := liveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	runnerID := "run_live_orphan"

	instance, err := first.Spawn(ctx, provider.SpawnOptions{
		RunnerID: runnerID,
		Name:     "marionette-live-orphan",
	})
	require.NoError(t, err)
	persistedID := instance.ProviderID

	// A second provider stands in for the restarted server: same account, no
	// cache. Cleanup goes through it, so a failure mid-test still stops the
	// billing.
	second := liveProvider(t)
	t.Cleanup(func() { killSandbox(t, second, runnerID, persistedID) })

	require.NoError(t, first.Pause(ctx, runnerID))
	t.Logf("paused sandbox %s; resuming from a provider that has never seen it", persistedID)

	resumed, err := second.Resume(ctx, "sess_live", provider.ResumeOptions{
		RunnerID:           runnerID,
		ProviderInstanceID: persistedID,
		// Present on purpose: if the persisted id were ignored, resume would
		// fall through to spawning this instead, and the assertion below is
		// what catches it.
		SpawnOpts: &provider.SpawnOptions{RunnerID: "run_live_orphan_replacement"},
	})
	require.NoError(t, err)

	assert.Equal(t, persistedID, resumed.ProviderID,
		"resume must wake the persisted sandbox rather than spawn a replacement")
	assert.Equal(t, runnerID, resumed.ID)

	if resumed.ProviderID != persistedID {
		// A replacement was spawned; kill it too so the account is left clean.
		killSandbox(t, second, "run_live_orphan_replacement", resumed.ProviderID)
	}
}
