package e2b

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/provider"
)

// fakeE2B is an E2B API that behaves the way the live one was verified to
// behave on 2026-08-23: pause returns 204, resume returns 201, and a PAUSED
// SANDBOX IS ABSENT FROM GET /sandboxes. That last property is the whole
// reason this file exists.
type fakeE2B struct {
	mu sync.Mutex

	// sandboxes maps sandboxID -> runnerID for running sandboxes only.
	sandboxes map[string]string
	// paused holds sandboxes that are paused. They are deliberately not
	// served by list or get.
	paused map[string]string

	nextID int

	listCalls int
}

func newFakeE2B() *fakeE2B {
	return &fakeE2B{
		sandboxes: map[string]string{},
		paused:    map[string]string{},
	}
}

func (f *fakeE2B) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sandboxes":
			var req CreateSandboxRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.nextID++
			sandboxID := "sandbox-" + string(rune('a'+f.nextID-1))
			f.sandboxes[sandboxID] = req.Metadata["marionette.dev/runner-id"]
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(CreateSandboxResponse{
				SandboxID:  sandboxID,
				TemplateID: req.TemplateID,
			})

		case r.Method == http.MethodGet && r.URL.Path == "/sandboxes":
			f.listCalls++
			out := []Sandbox{}
			for sandboxID, runnerID := range f.sandboxes {
				out = append(out, Sandbox{
					SandboxID: sandboxID,
					Metadata:  map[string]string{"marionette.dev/runner-id": runnerID},
				})
			}
			_ = json.NewEncoder(w).Encode(out)

		case r.Method == http.MethodPost && pathIs(r, "/pause"):
			sandboxID := sandboxIDFrom(r, "/pause")
			runnerID, ok := f.sandboxes[sandboxID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(f.sandboxes, sandboxID)
			f.paused[sandboxID] = runnerID
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && pathIs(r, "/resume"):
			sandboxID := sandboxIDFrom(r, "/resume")
			runnerID, ok := f.paused[sandboxID]
			if !ok {
				if _, running := f.sandboxes[sandboxID]; running {
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(APIError{Message: "already running"})
					return
				}
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(f.paused, sandboxID)
			f.sandboxes[sandboxID] = runnerID
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(Sandbox{SandboxID: sandboxID, TemplateID: "base"})

		case r.Method == http.MethodDelete:
			sandboxID := r.URL.Path[len("/sandboxes/"):]
			_, running := f.sandboxes[sandboxID]
			_, isPaused := f.paused[sandboxID]
			if !running && !isPaused {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(f.sandboxes, sandboxID)
			delete(f.paused, sandboxID)
			w.WriteHeader(http.StatusNoContent)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func pathIs(r *http.Request, suffix string) bool {
	return len(r.URL.Path) > len("/sandboxes/")+len(suffix) &&
		r.URL.Path[:len("/sandboxes/")] == "/sandboxes/" &&
		r.URL.Path[len(r.URL.Path)-len(suffix):] == suffix
}

func sandboxIDFrom(r *http.Request, suffix string) string {
	return r.URL.Path[len("/sandboxes/") : len(r.URL.Path)-len(suffix)]
}

// TestPausedSandboxSurvivesServerRestart is the regression for the orphan the
// live smoke found: pause a sandbox, throw the provider away (a server
// restart, so the in-memory cache is empty), and resume it.
//
// Before the persisted id, the new provider's only way to find the sandbox was
// GET /sandboxes, which does not list paused sandboxes. Resume therefore
// concluded the sandbox was gone and spawned a replacement, leaving the paused
// one running and billing with nothing left that could name it.
func TestPausedSandboxSurvivesServerRestart(t *testing.T) {
	api := newFakeE2B()
	server := httptest.NewServer(api.handler())
	defer server.Close()

	// --- process 1: spawn and pause ---
	first := newTestProvider(t, server.URL)

	instance, err := first.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID: "run_123",
		Name:     "runner-1",
	})
	require.NoError(t, err)
	require.NotEmpty(t, instance.ProviderID)

	// This is the id the server persists to runners.provider_instance_id.
	persistedID := instance.ProviderID

	_, err = first.Suspend(context.Background(), "run_123", provider.SuspendOptions{
		Strategy:           provider.SuspendStrategyPause,
		ProviderInstanceID: persistedID,
	})
	require.NoError(t, err)

	// The sandbox is now invisible to the API's list, which is what makes the
	// in-memory cache the only handle a restarted process could have had.
	require.Empty(t, api.sandboxes, "a paused sandbox must not be listed")
	require.Len(t, api.paused, 1)

	// --- process 2: a fresh provider with an empty cache ---
	second := newTestProvider(t, server.URL)

	resumed, err := second.Resume(context.Background(), "sess_1", provider.ResumeOptions{
		RunnerID:           "run_123",
		ProviderInstanceID: persistedID,
		SpawnOpts:          &provider.SpawnOptions{RunnerID: "run_456"},
	})
	require.NoError(t, err)

	assert.Equal(t, persistedID, resumed.ProviderID,
		"resume must wake the persisted sandbox, not spawn a replacement")
	assert.Equal(t, "run_123", resumed.ID)
	assert.Empty(t, api.paused, "the paused sandbox must have been resumed")
	assert.Len(t, api.sandboxes, 1, "resume must not leave a second sandbox behind")
}

// TestPausedSandboxCanBeDestroyedAfterRestart covers the other half: an
// operator killing a paused sandbox from a restarted server. Without the
// persisted id the kill cannot find it, and the sandbox bills forever.
func TestPausedSandboxCanBeDestroyedAfterRestart(t *testing.T) {
	api := newFakeE2B()
	server := httptest.NewServer(api.handler())
	defer server.Close()

	first := newTestProvider(t, server.URL)
	instance, err := first.Spawn(context.Background(), provider.SpawnOptions{RunnerID: "run_123"})
	require.NoError(t, err)
	require.NoError(t, first.Pause(context.Background(), "run_123"))
	require.Len(t, api.paused, 1)

	second := newTestProvider(t, server.URL)
	require.NoError(t, second.Destroy(context.Background(), "run_123", provider.DestroyOptions{
		ProviderInstanceID: instance.ProviderID,
	}))

	assert.Empty(t, api.paused, "a paused sandbox must be killable by its persisted id")
	assert.Empty(t, api.sandboxes)
}

// TestRestartWithoutPersistedIDStillSpawns documents what happens for runners
// spawned before the id was persisted: the provider falls back to the list,
// finds nothing, and spawns a replacement. The old sandbox is orphaned - that
// is the bug, and it is only reachable when the id is missing.
func TestRestartWithoutPersistedIDSpawnsReplacement(t *testing.T) {
	api := newFakeE2B()
	server := httptest.NewServer(api.handler())
	defer server.Close()

	first := newTestProvider(t, server.URL)
	_, err := first.Spawn(context.Background(), provider.SpawnOptions{RunnerID: "run_123"})
	require.NoError(t, err)
	require.NoError(t, first.Pause(context.Background(), "run_123"))

	second := newTestProvider(t, server.URL)
	resumed, err := second.Resume(context.Background(), "sess_1", provider.ResumeOptions{
		RunnerID:  "run_123",
		SpawnOpts: &provider.SpawnOptions{RunnerID: "run_456"},
	})
	require.NoError(t, err)

	assert.Equal(t, "run_456", resumed.ID, "with no persisted id, resume can only spawn anew")
	assert.Len(t, api.paused, 1, "and the paused sandbox is orphaned, which is the bug")
}

// TestPersistedIDSkipsTheList: the persisted id is used directly, so the blind
// list call is not even made.
func TestPersistedIDSkipsTheList(t *testing.T) {
	api := newFakeE2B()
	server := httptest.NewServer(api.handler())
	defer server.Close()

	p := newTestProvider(t, server.URL)
	instance, err := p.Spawn(context.Background(), provider.SpawnOptions{RunnerID: "run_123"})
	require.NoError(t, err)

	fresh := newTestProvider(t, server.URL)
	require.NoError(t, fresh.Destroy(context.Background(), "run_123", provider.DestroyOptions{
		ProviderInstanceID: instance.ProviderID,
	}))

	assert.Zero(t, api.listCalls, "a known instance id must not need an enumeration")
}
