package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests drive the real cobra commands through a real HTTPAdminClient
// against an httptest admin server, so flag parsing, name resolution, the
// HTTP call, the decode and the rendering are all exercised end to end. Only
// the server is fake, and the runner bodies it serves are the fixtures
// generated from the server's own response types in pkg/client/testdata -
// the same ones the SDK decode tests use, so a wire-shape change breaks both
// rather than leaving this one asserting a shape nobody sends.
//
// This is the httptest harness the R6 brief allows in place of a live admin
// server; no live server was run for these.

// adminFixture reads a generated admin fixture from the SDK's testdata.
func adminFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "pkg", "client", "testdata", name))
	require.NoError(t, err)
	return data
}

// fakeAdminServer records what the CLI asked for and answers with fixtures.
type fakeAdminServer struct {
	// spawnBody is the decoded body of the last spawn request.
	spawnBody map[string]any
	// listQuery is the raw query of the last runner list request.
	listQuery string
	// destroyed is the ID of the last destroyed runner.
	destroyed string
	// providerConfigs and profiles back the name resolution.
	providerConfigs []*client.ProviderConfig
	profiles        []*client.Profile
	// providerConfigPages, when set, is served one page per request, each but
	// the last carrying a next_cursor.
	providerConfigPages [][]*client.ProviderConfig
	// providerConfigCursors records the cursor each list request carried.
	providerConfigCursors []string
	// providerConfigError, when set, is returned instead of a page.
	providerConfigError int
	// profileError, when set, is returned instead of the profile list.
	profileError int
	// runnerError, when set, is returned instead of a runner response.
	runnerError int
}

func (f *fakeAdminServer) start(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		w.Header().Set("Content-Type", "application/json")

		switch {
		case path == "/admin/api/v1/provider-configs" && r.Method == http.MethodGet:
			f.providerConfigCursors = append(f.providerConfigCursors, r.URL.Query().Get("cursor"))
			if f.providerConfigError != 0 {
				w.WriteHeader(f.providerConfigError)
				_, _ = w.Write([]byte(`{"code":"internal_error","message":"provider config store is down"}`))
				return
			}
			if pages := f.providerConfigPages; len(pages) > 0 {
				n := min(len(f.providerConfigCursors)-1, len(pages)-1)
				next := ""
				if n < len(pages)-1 {
					next = fmt.Sprintf("cursor_%d", n+1)
				}
				writeJSON(t, w, http.StatusOK, client.ListResult[client.ProviderConfig]{
					Items:      pages[n],
					TotalCount: int64(len(pages[n])),
					HasMore:    next != "",
					NextCursor: next,
				})
				return
			}
			writeJSON(t, w, http.StatusOK, client.ListResult[client.ProviderConfig]{
				Items:      f.providerConfigs,
				TotalCount: int64(len(f.providerConfigs)),
			})

		case path == "/admin/api/v1/profiles" && r.Method == http.MethodGet:
			if f.profileError != 0 {
				w.WriteHeader(f.profileError)
				_, _ = w.Write([]byte(`{"code":"internal_error","message":"profile store is down"}`))
				return
			}
			writeJSON(t, w, http.StatusOK, client.ListResult[client.Profile]{
				Items:      f.profiles,
				TotalCount: int64(len(f.profiles)),
			})

		case path == "/admin/api/v1/runners/spawn" && r.Method == http.MethodPost:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&f.spawnBody))
			if f.runnerError != 0 {
				w.WriteHeader(f.runnerError)
				_, _ = w.Write([]byte(`{"code":"validation_error","message":"provider config not found"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(adminFixture(t, "runner_response.json"))

		case path == "/admin/api/v1/runners" && r.Method == http.MethodGet:
			f.listQuery = r.URL.RawQuery
			_, _ = w.Write(adminFixture(t, "runner_list_response.json"))

		case strings.HasPrefix(path, "/admin/api/v1/runners/") && r.Method == http.MethodGet:
			if f.runnerError != 0 {
				w.WriteHeader(f.runnerError)
				_, _ = w.Write([]byte(`{"code":"not_found","message":"runner not found"}`))
				return
			}
			_, _ = w.Write(adminFixture(t, "runner_response.json"))

		case strings.HasPrefix(path, "/admin/api/v1/runners/") && r.Method == http.MethodDelete:
			f.destroyed = strings.TrimPrefix(path, "/admin/api/v1/runners/")
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return server.URL
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

// resetRunnerFlags returns the runner commands' flag variables to their zero
// values. Cobra binds flags to package variables, so within one test process a
// value set by an earlier case survives into the next Execute; a real mctl run
// parses its flags once and exits, so only the harness has to undo it.
func resetRunnerFlags() {
	spawnRunnerName = ""
	spawnRunnerProviderConfig = ""
	spawnRunnerProfile = ""
	spawnRunnerLabels = nil
	listRunnerStatus = nil
	listRunnerPoolName = ""
	listRunnerLabels = nil
	listRunnerLimit = 50
	listRunnerCursor = ""
}

// runMctl executes the real command tree and returns everything it printed.
func runMctl(t *testing.T, adminURL string, args ...string) (string, error) {
	t.Helper()

	resetRunnerFlags()
	SetAdminClient(client.NewHTTPAdminClient(adminURL, "admin", "secret"))
	t.Cleanup(func() {
		ResetAdminClient()
		// The output flag is a package variable bound to a persistent flag, so
		// a `-o json` case would otherwise leak into whatever runs next.
		outputFmt = "table"
	})

	var err error
	out := captureOutput(t, func() {
		rootCmd.SetArgs(args)
		err = rootCmd.Execute()
	})

	return out, err
}

func TestAdminRunnersList_RendersTheOperatorView(t *testing.T) {
	fake := &fakeAdminServer{}
	url := fake.start(t)

	out, err := runMctl(t, url, "admin", "runners", "list")
	require.NoError(t, err)

	// The two columns the admin view exists for.
	assert.Contains(t, out, "PROVIDER")
	assert.Contains(t, out, "pcfg_0002xK9mNsY4VwJaU1")

	assert.Contains(t, out, "run_0002xK9mNpV1StGXR8")
	assert.Contains(t, out, "docker-runner-1")
	assert.Contains(t, out, "gpu-pool")
	assert.Contains(t, out, "runner-is-sandbox")

	// The tainted second row must say so; "offline" alone would read as a
	// runner that is merely away.
	assert.Contains(t, out, "offline (tainted)")

	// The fixture has another page.
	assert.Contains(t, out, "--cursor cursor_0002xK9mNvB7")
}

func TestAdminRunnersList_PassesFiltersToTheAdminAPI(t *testing.T) {
	fake := &fakeAdminServer{}
	url := fake.start(t)

	_, err := runMctl(t, url, "admin", "runners", "list",
		"--status", "idle,busy", "--pool-name", "gpu-pool", "--label", "env=prod", "--limit", "10")
	require.NoError(t, err)

	assert.Contains(t, fake.listQuery, "status=idle%2Cbusy")
	assert.Contains(t, fake.listQuery, "pool_name=gpu-pool")
	assert.Contains(t, fake.listQuery, "labels=env%3Dprod")
	assert.Contains(t, fake.listQuery, "limit=10")
}

// -o json has to come out as a document, not a table with prose around it.
func TestAdminRunnersList_JSONOutputParses(t *testing.T) {
	fake := &fakeAdminServer{}
	url := fake.start(t)

	out, err := runMctl(t, url, "admin", "runners", "list", "-o", "json")
	require.NoError(t, err)

	var runners []*client.AdminRunner
	require.NoError(t, json.Unmarshal([]byte(out), &runners), "output must be valid JSON")
	require.Len(t, runners, 2)
	assert.Equal(t, "run_0002xK9mNpV1StGXR8", runners[0].ID)
	require.NotNil(t, runners[0].ProviderInstanceID)
	assert.Equal(t, "b3f1c9d2e4a5", *runners[0].ProviderInstanceID)
}

func TestAdminRunnersList_YAMLOutput(t *testing.T) {
	fake := &fakeAdminServer{}
	url := fake.start(t)

	out, err := runMctl(t, url, "admin", "runners", "list", "-o", "yaml")
	require.NoError(t, err)

	assert.Contains(t, out, "id: run_0002xK9mNpV1StGXR8")
	assert.NotContains(t, out, "More results available", "prose does not belong in a document")
}

func TestAdminRunnersList_EmptyIsNotAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"total_count":0,"has_more":false}`))
	}))
	defer server.Close()

	out, err := runMctl(t, server.URL, "admin", "runners", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No runners found.")
}

func TestAdminRunnersGet(t *testing.T) {
	fake := &fakeAdminServer{}
	url := fake.start(t)

	out, err := runMctl(t, url, "admin", "runners", "get", "run_0002xK9mNpV1StGXR8")
	require.NoError(t, err)

	assert.Contains(t, out, "run_0002xK9mNpV1StGXR8")
	assert.Contains(t, out, "docker-runner-1")
}

func TestAdminRunnersGet_NotFoundIsReported(t *testing.T) {
	fake := &fakeAdminServer{runnerError: http.StatusNotFound}
	url := fake.start(t)

	_, err := runMctl(t, url, "admin", "runners", "get", "run_missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get runner")
	assert.Contains(t, err.Error(), "runner not found")
}

// The point of --provider-config taking a name: an operator has the name from
// the config file, not the generated ID.
func TestAdminRunnersSpawn_ResolvesProviderConfigAndProfileNames(t *testing.T) {
	fake := &fakeAdminServer{
		providerConfigs: []*client.ProviderConfig{
			{ID: "pcfg_other", Name: "k8s-staging"},
			{ID: "pcfg_docker", Name: "docker-local"},
		},
		profiles: []*client.Profile{
			{ID: "prof_small", Name: "dev-small"},
		},
	}
	url := fake.start(t)

	out, err := runMctl(t, url, "admin", "runners", "spawn",
		"--provider-config", "docker-local",
		"--profile", "dev-small",
		"--name", "runner-1",
		"--label", "env=dev")
	require.NoError(t, err)

	assert.Equal(t, "pcfg_docker", fake.spawnBody["provider_config_id"])
	assert.Equal(t, "prof_small", fake.spawnBody["profile_id"])
	assert.Equal(t, "runner-1", fake.spawnBody["name"])
	assert.Equal(t, map[string]any{"env": "dev"}, fake.spawnBody["labels"])

	assert.Contains(t, out, "run_0002xK9mNpV1StGXR8")
}

// An ID is passed through untouched: no list call is needed, and the fake
// would fail the test if one were made without configured items.
func TestAdminRunnersSpawn_AcceptsIDsDirectly(t *testing.T) {
	fake := &fakeAdminServer{}
	url := fake.start(t)

	_, err := runMctl(t, url, "admin", "runners", "spawn",
		"--provider-config", "pcfg_0002xK9mNsY4VwJaU1",
		"--profile", "prof_0002xK9mNtZ5WxKbV2")
	require.NoError(t, err)

	assert.Equal(t, "pcfg_0002xK9mNsY4VwJaU1", fake.spawnBody["provider_config_id"])
	assert.Equal(t, "prof_0002xK9mNtZ5WxKbV2", fake.spawnBody["profile_id"])
	assert.NotContains(t, fake.spawnBody, "name")
}

func TestAdminRunnersSpawn_UnknownNameSaysSo(t *testing.T) {
	fake := &fakeAdminServer{
		providerConfigs: []*client.ProviderConfig{{ID: "pcfg_docker", Name: "docker-local"}},
	}
	url := fake.start(t)

	_, err := runMctl(t, url, "admin", "runners", "spawn", "--provider-config", "typo-local")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no provider config named "typo-local"`)
	assert.Nil(t, fake.spawnBody, "nothing may be spawned when the name did not resolve")
}

// Two configs with one name is ambiguous; picking one silently would spawn a
// runner on whichever provider happened to sort first.
func TestAdminRunnersSpawn_AmbiguousNameIsRefused(t *testing.T) {
	fake := &fakeAdminServer{
		providerConfigs: []*client.ProviderConfig{
			{ID: "pcfg_a", Name: "docker-local"},
			{ID: "pcfg_b", Name: "docker-local"},
		},
	}
	url := fake.start(t)

	_, err := runMctl(t, url, "admin", "runners", "spawn", "--provider-config", "docker-local")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 provider configs are named")
	assert.Contains(t, err.Error(), "pcfg_a, pcfg_b")
	assert.Nil(t, fake.spawnBody)
}

// A name on the second page must still resolve: the admin API caps a page at
// 100 provider configs, and stopping at the first page would report a config
// that exists as missing.
func TestAdminRunnersSpawn_ResolvesANameOnALaterPage(t *testing.T) {
	fake := &fakeAdminServer{
		providerConfigPages: [][]*client.ProviderConfig{
			{{ID: "pcfg_a", Name: "k8s-staging"}},
			{{ID: "pcfg_b", Name: "docker-local"}},
		},
	}
	url := fake.start(t)

	_, err := runMctl(t, url, "admin", "runners", "spawn", "--provider-config", "docker-local")
	require.NoError(t, err)

	assert.Equal(t, "pcfg_b", fake.spawnBody["provider_config_id"])
	assert.Equal(t, []string{"", "cursor_1"}, fake.providerConfigCursors,
		"the second request must carry the cursor the first returned")
}

func TestAdminRunnersSpawn_ResolutionFailureIsReported(t *testing.T) {
	fake := &fakeAdminServer{providerConfigError: http.StatusInternalServerError}
	url := fake.start(t)

	_, err := runMctl(t, url, "admin", "runners", "spawn", "--provider-config", "docker-local")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to resolve provider config "docker-local"`)
	assert.Nil(t, fake.spawnBody, "a lookup that failed must not spawn anything")
}

func TestAdminRunnersSpawn_ProfileResolutionFailureIsReported(t *testing.T) {
	fake := &fakeAdminServer{
		providerConfigs: []*client.ProviderConfig{{ID: "pcfg_docker", Name: "docker-local"}},
		profileError:    http.StatusInternalServerError,
	}
	url := fake.start(t)

	_, err := runMctl(t, url, "admin", "runners", "spawn",
		"--provider-config", "docker-local", "--profile", "dev-small")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to resolve profile "dev-small"`)
	assert.Nil(t, fake.spawnBody)
}

func TestAdminRunnersSpawn_ServerErrorIsReported(t *testing.T) {
	fake := &fakeAdminServer{runnerError: http.StatusBadRequest}
	url := fake.start(t)

	_, err := runMctl(t, url, "admin", "runners", "spawn", "--provider-config", "pcfg_gone")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to spawn runner")
	assert.Contains(t, err.Error(), "provider config not found")
}

func TestAdminRunnersDestroy(t *testing.T) {
	fake := &fakeAdminServer{}
	url := fake.start(t)

	out, err := runMctl(t, url, "admin", "runners", "destroy", "run_0002xK9mNpV1StGXR8")
	require.NoError(t, err)

	assert.Equal(t, "run_0002xK9mNpV1StGXR8", fake.destroyed)
	assert.Contains(t, out, "destroyed")
}

func TestAdminRunners_WithoutAClientIsAnError(t *testing.T) {
	ResetAdminClient()
	t.Cleanup(ResetAdminClient)

	// PersistentPreRunE refuses before any subcommand runs: no admin server
	// and no credentials means there is nothing to talk to.
	rootCmd.SetArgs([]string{"admin", "runners", "list"})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "admin")
}
