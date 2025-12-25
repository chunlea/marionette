package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

var testStore *pgstore.Store

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start PostgreSQL container
	postgresContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("marionette_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}

	// Get connection string
	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic("failed to get connection string: " + err.Error())
	}

	// Create logger
	logger, _ := zap.NewDevelopment()

	// Create store
	testStore, err = pgstore.New(ctx, pgstore.Config{URL: connStr}, logger)
	if err != nil {
		panic("failed to create store: " + err.Error())
	}

	// Run migrations
	if err := runMigrations(ctx, testStore); err != nil {
		panic("failed to run migrations: " + err.Error())
	}

	// Run tests
	code := m.Run()

	// Cleanup
	_ = testStore.Close()
	_ = postgresContainer.Terminate(ctx)

	os.Exit(code)
}

func runMigrations(ctx context.Context, s *pgstore.Store) error {
	// Read and execute migration file
	migration, err := os.ReadFile("../../../migrations/001_initial.up.sql")
	if err != nil {
		return err
	}
	return s.ExecRaw(ctx, string(migration))
}

// =============================================================================
// Runner Tests
// =============================================================================

func TestRunnerCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	runner := &store.Runner{
		Name:         "test-runner-" + time.Now().Format("150405"),
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{}, // Required, NOT NULL
		SandboxTypes: []string{}, // Required
	}

	err := testStore.CreateRunner(ctx, runner)
	require.NoError(t, err)
	assert.NotEmpty(t, runner.ID)
	assert.NotZero(t, runner.CreatedAt)

	// Get
	got, err := testStore.GetRunner(ctx, runner.ID)
	require.NoError(t, err)
	assert.Equal(t, runner.Name, got.Name)
	assert.Equal(t, runner.Hostname, got.Hostname)

	// Update
	newStatus := "idle"
	err = testStore.UpdateRunner(ctx, runner.ID, store.RunnerUpdates{
		Status: &newStatus,
	})
	require.NoError(t, err)

	got, err = testStore.GetRunner(ctx, runner.ID)
	require.NoError(t, err)
	assert.Equal(t, "idle", got.Status)

	// List
	list, err := testStore.ListRunners(ctx, store.ListRunnersOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// Delete
	err = testStore.DeleteRunner(ctx, runner.ID)
	require.NoError(t, err)

	_, err = testStore.GetRunner(ctx, runner.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRunnerNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := testStore.GetRunner(ctx, "run_nonexistent12345")
	assert.ErrorIs(t, err, store.ErrNotFound)

	var notFoundErr *store.NotFoundError
	assert.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "runner", notFoundErr.Resource)
}

// =============================================================================
// Workspace Tests
// =============================================================================

func TestWorkspaceCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	workspace := &store.Workspace{
		Name:        "test-workspace-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}

	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)
	assert.NotEmpty(t, workspace.ID)

	// Get
	got, err := testStore.GetWorkspace(ctx, workspace.ID)
	require.NoError(t, err)
	assert.Equal(t, workspace.Name, got.Name)

	// Update
	newMobility := "shared"
	err = testStore.UpdateWorkspace(ctx, workspace.ID, store.WorkspaceUpdates{
		Mobility: &newMobility,
	})
	require.NoError(t, err)

	got, err = testStore.GetWorkspace(ctx, workspace.ID)
	require.NoError(t, err)
	assert.Equal(t, "shared", got.Mobility)

	// Soft delete
	err = testStore.DeleteWorkspace(ctx, workspace.ID)
	require.NoError(t, err)

	// Should not appear in list without IncludeDeleted
	list, err := testStore.ListWorkspaces(ctx, store.ListWorkspacesOptions{})
	require.NoError(t, err)
	for _, w := range list.Items {
		assert.NotEqual(t, workspace.ID, w.ID)
	}

	// Should appear with IncludeDeleted
	list, err = testStore.ListWorkspaces(ctx, store.ListWorkspacesOptions{
		IncludeDeleted: true,
	})
	require.NoError(t, err)
	found := false
	for _, w := range list.Items {
		if w.ID == workspace.ID {
			found = true
			assert.NotNil(t, w.DeletedAt)
		}
	}
	assert.True(t, found, "deleted workspace should appear with IncludeDeleted")
}

// =============================================================================
// Session Tests
// =============================================================================

func TestSessionCRUD(t *testing.T) {
	ctx := context.Background()

	// Create workspace first (required for session)
	workspace := &store.Workspace{
		Name:        "session-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create session
	session := &store.Session{
		Name:          strPtr("test-session"),
		Status:        "pending",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{}, // Required, NOT NULL
		LifecycleMode: "on_demand",
	}

	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)
	assert.NotEmpty(t, session.ID)

	// Get
	got, err := testStore.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, "claude", got.Agent)
	assert.Equal(t, workspace.ID, got.WorkspaceID)

	// Update
	newStatus := "active"
	err = testStore.UpdateSession(ctx, session.ID, store.SessionUpdates{
		Status: &newStatus,
	})
	require.NoError(t, err)

	got, err = testStore.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", got.Status)

	// List
	list, err := testStore.ListSessions(ctx, store.ListSessionsOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// Delete
	err = testStore.DeleteSession(ctx, session.ID)
	require.NoError(t, err)

	_, err = testStore.GetSession(ctx, session.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// =============================================================================
// Task Tests
// =============================================================================

func TestTaskCRUD(t *testing.T) {
	ctx := context.Background()

	// Create workspace and session first
	workspace := &store.Workspace{
		Name:        "task-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{}, // Required, NOT NULL
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create task
	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Test prompt",
		Status:         "pending",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}

	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)
	assert.NotEmpty(t, task.ID)

	// Get
	got, err := testStore.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "Test prompt", got.Prompt)

	// Create task run
	taskRun := &store.TaskRun{
		TaskID:  task.ID,
		Attempt: 1,
		Status:  "pending",
	}

	err = testStore.CreateTaskRun(ctx, taskRun)
	require.NoError(t, err)
	assert.NotEmpty(t, taskRun.ID)

	// Get task run
	gotRun, err := testStore.GetTaskRun(ctx, taskRun.ID)
	require.NoError(t, err)
	assert.Equal(t, task.ID, gotRun.TaskID)
	assert.Equal(t, 1, gotRun.Attempt)

	// List task runs
	runs, err := testStore.ListTaskRuns(ctx, store.ListTaskRunsOptions{
		TaskID: &task.ID,
	})
	require.NoError(t, err)
	assert.Len(t, runs.Items, 1)
}

// =============================================================================
// Transaction Tests
// =============================================================================

func TestTransaction(t *testing.T) {
	ctx := context.Background()

	// Test successful transaction
	t.Run("commit", func(t *testing.T) {
		tx, err := testStore.BeginTx(ctx)
		require.NoError(t, err)

		runner := &store.Runner{
			Name:         "tx-test-runner-" + time.Now().Format("150405.000"),
			Hostname:     "localhost",
			Status:       "offline",
			SandboxMode:  "runner-is-sandbox",
			Capabilities: []string{},
			SandboxTypes: []string{},
		}
		err = tx.CreateRunner(ctx, runner)
		require.NoError(t, err)

		err = tx.Commit(ctx)
		require.NoError(t, err)

		// Verify runner exists
		got, err := testStore.GetRunner(ctx, runner.ID)
		require.NoError(t, err)
		assert.Equal(t, runner.Name, got.Name)

		// Cleanup
		_ = testStore.DeleteRunner(ctx, runner.ID)
	})

	// Test rollback
	t.Run("rollback", func(t *testing.T) {
		tx, err := testStore.BeginTx(ctx)
		require.NoError(t, err)

		runner := &store.Runner{
			Name:         "tx-rollback-runner-" + time.Now().Format("150405.000"),
			Hostname:     "localhost",
			Status:       "offline",
			SandboxMode:  "runner-is-sandbox",
			Capabilities: []string{},
			SandboxTypes: []string{},
		}
		err = tx.CreateRunner(ctx, runner)
		require.NoError(t, err)

		// Rollback instead of commit
		err = tx.Rollback(ctx)
		require.NoError(t, err)

		// Verify runner does not exist
		_, err = testStore.GetRunner(ctx, runner.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

// =============================================================================
// API Key Tests
// =============================================================================

func TestAPIKeyCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	apiKey := &store.APIKey{
		Name:        "test-key-" + time.Now().Format("150405"),
		KeyHash:     "sha256-test-hash-" + time.Now().Format("150405"),
		KeyPrefix:   "mk_test1234",
		HashVersion: 1,
		Scopes:      []string{"read", "write"},
	}

	err := testStore.CreateAPIKey(ctx, apiKey)
	require.NoError(t, err)
	assert.NotEmpty(t, apiKey.ID)

	// Get
	got, err := testStore.GetAPIKey(ctx, apiKey.ID)
	require.NoError(t, err)
	assert.Equal(t, apiKey.Name, got.Name)
	assert.Equal(t, apiKey.KeyHash, got.KeyHash)

	// Get by hash
	got, err = testStore.GetAPIKeyByHash(ctx, apiKey.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, apiKey.ID, got.ID)

	// Update
	newName := "updated-key"
	err = testStore.UpdateAPIKey(ctx, apiKey.ID, store.APIKeyUpdates{
		Name: &newName,
	})
	require.NoError(t, err)

	got, err = testStore.GetAPIKey(ctx, apiKey.ID)
	require.NoError(t, err)
	assert.Equal(t, "updated-key", got.Name)

	// Delete
	err = testStore.DeleteAPIKey(ctx, apiKey.ID)
	require.NoError(t, err)

	_, err = testStore.GetAPIKey(ctx, apiKey.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// =============================================================================
// Config Tests
// =============================================================================

func TestProviderConfigCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	config := &store.ProviderConfig{
		Name:     "test-provider-" + time.Now().Format("150405"),
		Provider: "docker",
	}

	err := testStore.CreateProviderConfig(ctx, config)
	require.NoError(t, err)
	assert.NotEmpty(t, config.ID)

	// Get
	got, err := testStore.GetProviderConfig(ctx, config.ID)
	require.NoError(t, err)
	assert.Equal(t, "docker", got.Provider)

	// Update
	isDefault := true
	err = testStore.UpdateProviderConfig(ctx, config.ID, store.ProviderConfigUpdates{
		IsDefault: &isDefault,
	})
	require.NoError(t, err)

	got, err = testStore.GetProviderConfig(ctx, config.ID)
	require.NoError(t, err)
	assert.True(t, got.IsDefault)

	// Get default
	defaultConfig, err := testStore.GetDefaultProviderConfig(ctx, "docker")
	require.NoError(t, err)
	assert.Equal(t, config.ID, defaultConfig.ID)

	// Delete
	err = testStore.DeleteProviderConfig(ctx, config.ID)
	require.NoError(t, err)
}

// =============================================================================
// Pagination Tests
// =============================================================================

func TestPagination(t *testing.T) {
	ctx := context.Background()

	// Create multiple runners
	var createdIDs []string
	for i := 0; i < 5; i++ {
		runner := &store.Runner{
			Name:         "pagination-test-" + time.Now().Format("150405.000000"),
			Hostname:     "localhost",
			Status:       "offline",
			SandboxMode:  "runner-is-sandbox",
			Capabilities: []string{},
			SandboxTypes: []string{},
		}
		err := testStore.CreateRunner(ctx, runner)
		require.NoError(t, err)
		createdIDs = append(createdIDs, runner.ID)
		time.Sleep(time.Millisecond) // Ensure different names
	}

	// Test limit
	list, err := testStore.ListRunners(ctx, store.ListRunnersOptions{
		BaseListOptions: store.BaseListOptions{
			Limit: 2,
		},
	})
	require.NoError(t, err)
	assert.Len(t, list.Items, 2)
	assert.True(t, list.HasMore)

	// Cleanup
	for _, id := range createdIDs {
		_ = testStore.DeleteRunner(ctx, id)
	}
}

// =============================================================================
// Error Tests
// =============================================================================

func TestAlreadyExistsError(t *testing.T) {
	ctx := context.Background()

	name := "duplicate-runner-" + time.Now().Format("150405")

	// Create first runner
	runner1 := &store.Runner{
		Name:         name,
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	err := testStore.CreateRunner(ctx, runner1)
	require.NoError(t, err)

	// Try to create duplicate
	runner2 := &store.Runner{
		Name:         name,
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	err = testStore.CreateRunner(ctx, runner2)
	assert.ErrorIs(t, err, store.ErrAlreadyExists)

	// Cleanup
	_ = testStore.DeleteRunner(ctx, runner1.ID)
}

// =============================================================================
// Helper Functions
// =============================================================================

func strPtr(s string) *string {
	return &s
}
