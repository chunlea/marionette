package grpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
	mockstore "github.com/chunlea/marionette/pkg/store/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// integrationTestStore implements store.Store for integration testing.
// Uses mutex + copy-on-read to avoid race conditions in concurrent tests.
type integrationTestStore struct {
	mu           sync.RWMutex
	runners      map[string]*store.Runner
	runnerByName map[string]*store.Runner
	nextID       int
}

func newIntegrationTestStore() *integrationTestStore {
	return &integrationTestStore{
		runners:      make(map[string]*store.Runner),
		runnerByName: make(map[string]*store.Runner),
		nextID:       1,
	}
}

func (s *integrationTestStore) CreateRunner(_ context.Context, runner *store.Runner) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if runner.ID == "" {
		runner.ID = fmt.Sprintf("run_test%d", s.nextID)
		s.nextID++
	}
	// Store a copy to avoid external mutations
	copy := *runner
	s.runners[runner.ID] = &copy
	if runner.Name != "" {
		s.runnerByName[runner.Name] = &copy
	}
	return nil
}

func (s *integrationTestStore) GetRunner(_ context.Context, id string) (*store.Runner, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runner, ok := s.runners[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	// Return a copy to avoid race conditions
	copy := *runner
	return &copy, nil
}

func (s *integrationTestStore) GetRunnerByName(_ context.Context, name string) (*store.Runner, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runner, ok := s.runnerByName[name]
	if !ok {
		return nil, store.ErrNotFound
	}
	// Return a copy to avoid race conditions
	copy := *runner
	return &copy, nil
}

func (s *integrationTestStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	runner, ok := s.runners[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		runner.Status = *updates.Status
	}
	if updates.Hostname != nil {
		runner.Hostname = *updates.Hostname
	}
	if updates.LastSeenAt != nil {
		runner.LastSeenAt = updates.LastSeenAt
	}
	return nil
}

func (s *integrationTestStore) ListRunners(_ context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]*store.Runner, 0, len(s.runners))
	for _, r := range s.runners {
		if len(opts.Status) > 0 {
			found := false
			for _, status := range opts.Status {
				if r.Status == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		// Return copies to avoid race conditions
		copy := *r
		items = append(items, &copy)
	}
	return &store.ListResult[store.Runner]{Items: items}, nil
}

func (s *integrationTestStore) DeleteRunner(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.runners, id)
	return nil
}

// Implement other store.Store methods as stubs
func (s *integrationTestStore) CreateWorkspace(_ context.Context, _ *store.Workspace) error {
	return nil
}
func (s *integrationTestStore) GetWorkspace(_ context.Context, _ string) (*store.Workspace, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListWorkspaces(_ context.Context, _ store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	return &store.ListResult[store.Workspace]{}, nil
}
func (s *integrationTestStore) UpdateWorkspace(_ context.Context, _ string, _ store.WorkspaceUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteWorkspace(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateSession(_ context.Context, _ *store.Session) error { return nil }
func (s *integrationTestStore) GetSession(_ context.Context, _ string) (*store.Session, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListSessions(_ context.Context, _ store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return &store.ListResult[store.Session]{}, nil
}
func (s *integrationTestStore) UpdateSession(_ context.Context, _ string, _ store.SessionUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteSession(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateTask(_ context.Context, _ *store.Task) error { return nil }
func (s *integrationTestStore) GetTask(_ context.Context, _ string) (*store.Task, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListTasks(_ context.Context, _ store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	return &store.ListResult[store.Task]{}, nil
}
func (s *integrationTestStore) UpdateTask(_ context.Context, _ string, _ store.TaskUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteTask(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateTaskRun(_ context.Context, _ *store.TaskRun) error { return nil }
func (s *integrationTestStore) GetTaskRun(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListTaskRuns(_ context.Context, _ store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	return &store.ListResult[store.TaskRun]{}, nil
}
func (s *integrationTestStore) UpdateTaskRun(_ context.Context, _ string, _ store.TaskRunUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteTaskRun(_ context.Context, _ string) error { return nil }
func (s *integrationTestStore) GetTaskRunByTaskAndAttempt(_ context.Context, _ string, _ int) (*store.TaskRun, error) {
	return nil, store.ErrNotFound
}

func (s *integrationTestStore) CreateAPIKey(_ context.Context, _ *store.APIKey) error { return nil }
func (s *integrationTestStore) GetAPIKey(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetAPIKeyByHash(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListAPIKeys(_ context.Context, _ store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	return &store.ListResult[store.APIKey]{}, nil
}
func (s *integrationTestStore) UpdateAPIKey(_ context.Context, _ string, _ store.APIKeyUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteAPIKey(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateRunnerToken(_ context.Context, _ *store.RunnerToken) error {
	return nil
}
func (s *integrationTestStore) GetRunnerToken(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetRunnerTokenByHash(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListRunnerTokens(_ context.Context, _ store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	return &store.ListResult[store.RunnerToken]{}, nil
}
func (s *integrationTestStore) UpdateRunnerToken(_ context.Context, _ string, _ store.RunnerTokenUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteRunnerToken(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateScheduledTask(_ context.Context, _ *store.ScheduledTask) error {
	return nil
}
func (s *integrationTestStore) GetScheduledTask(_ context.Context, _ string) (*store.ScheduledTask, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListScheduledTasks(_ context.Context, _ store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return &store.ListResult[store.ScheduledTask]{}, nil
}
func (s *integrationTestStore) UpdateScheduledTask(_ context.Context, _ string, _ store.ScheduledTaskUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteScheduledTask(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreatePermissionRequest(_ context.Context, _ *store.PermissionRequest) error {
	return nil
}
func (s *integrationTestStore) GetPermissionRequest(_ context.Context, _ string) (*store.PermissionRequest, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListPermissionRequests(_ context.Context, _ store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	return &store.ListResult[store.PermissionRequest]{}, nil
}
func (s *integrationTestStore) UpdatePermissionRequest(_ context.Context, _ string, _ store.PermissionRequestUpdates) error {
	return nil
}

func (s *integrationTestStore) CreateAgentConfig(_ context.Context, _ *store.AgentConfig) error {
	return nil
}
func (s *integrationTestStore) GetAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetAgentConfigByName(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetDefaultAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListAgentConfigs(_ context.Context, _ store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	return &store.ListResult[store.AgentConfig]{}, nil
}
func (s *integrationTestStore) UpdateAgentConfig(_ context.Context, _ string, _ store.AgentConfigUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteAgentConfig(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateProviderConfig(_ context.Context, _ *store.ProviderConfig) error {
	return nil
}
func (s *integrationTestStore) GetProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetProviderConfigByName(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetDefaultProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListProviderConfigs(_ context.Context, _ store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	return &store.ListResult[store.ProviderConfig]{}, nil
}
func (s *integrationTestStore) UpdateProviderConfig(_ context.Context, _ string, _ store.ProviderConfigUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteProviderConfig(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateProfile(_ context.Context, _ *store.Profile) error { return nil }
func (s *integrationTestStore) GetProfile(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetProfileByName(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListProfiles(_ context.Context, _ store.ListProfilesOptions) (*store.ListResult[store.Profile], error) {
	return &store.ListResult[store.Profile]{}, nil
}
func (s *integrationTestStore) UpdateProfile(_ context.Context, _ string, _ store.ProfileUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteProfile(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateSnapshot(_ context.Context, _ *store.Snapshot) error {
	return nil
}
func (s *integrationTestStore) GetSnapshot(_ context.Context, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetSnapshotByRunnerAndName(_ context.Context, _, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListSnapshots(_ context.Context, _ store.ListSnapshotsOptions) (*store.ListResult[store.Snapshot], error) {
	return &store.ListResult[store.Snapshot]{}, nil
}
func (s *integrationTestStore) UpdateSnapshot(_ context.Context, _ string, _ store.SnapshotUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteSnapshot(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateTunnel(_ context.Context, _ *store.Tunnel) error { return nil }
func (s *integrationTestStore) GetTunnel(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetTunnelByTokenHash(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListTunnels(_ context.Context, _ store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	return &store.ListResult[store.Tunnel]{}, nil
}
func (s *integrationTestStore) UpdateTunnel(_ context.Context, _ string, _ store.TunnelUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteTunnel(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateActionLog(_ context.Context, _ *store.ActionLog) error {
	return nil
}
func (s *integrationTestStore) GetActionLog(_ context.Context, _ string) (*store.ActionLog, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListActionLogs(_ context.Context, _ store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	return &store.ListResult[store.ActionLog]{}, nil
}

func (s *integrationTestStore) CreateLog(_ context.Context, _ *store.Log) error    { return nil }
func (s *integrationTestStore) CreateLogs(_ context.Context, _ []*store.Log) error { return nil }
func (s *integrationTestStore) ListLogs(_ context.Context, _ store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	return &store.ListResult[store.Log]{}, nil
}

func (s *integrationTestStore) CreateLogArchive(_ context.Context, _ *store.LogArchive) error {
	return nil
}
func (s *integrationTestStore) GetLogArchive(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetLogArchiveBySession(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) ListLogArchives(_ context.Context, _ store.ListLogArchivesOptions) (*store.ListResult[store.LogArchive], error) {
	return &store.ListResult[store.LogArchive]{}, nil
}
func (s *integrationTestStore) UpdateLogArchive(_ context.Context, _ string, _ store.LogArchiveUpdates) error {
	return nil
}

func (s *integrationTestStore) CreateDataKey(_ context.Context, _ *store.DataKey) error { return nil }
func (s *integrationTestStore) GetDataKey(_ context.Context, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetDataKeyByResource(_ context.Context, _, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) UpdateDataKey(_ context.Context, _ string, _ store.DataKeyUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteDataKey(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) CreateChunk(_ context.Context, _ *store.Chunk) error { return nil }
func (s *integrationTestStore) GetChunk(_ context.Context, _, _ string) (*store.Chunk, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) UpdateChunk(_ context.Context, _, _ string, _ store.ChunkUpdates) error {
	return nil
}
func (s *integrationTestStore) DeleteChunk(_ context.Context, _, _ string) error { return nil }
func (s *integrationTestStore) IncrementChunkRefCount(_ context.Context, _, _ string) error {
	return nil
}
func (s *integrationTestStore) DecrementChunkRefCount(_ context.Context, _, _ string) error {
	return nil
}

func (s *integrationTestStore) CreateManifest(_ context.Context, _ *store.Manifest) error {
	return nil
}
func (s *integrationTestStore) GetManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) GetLatestManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (s *integrationTestStore) DeleteManifest(_ context.Context, _ string) error { return nil }

func (s *integrationTestStore) BeginTx(_ context.Context) (store.Tx, error) { return nil, nil }
func (s *integrationTestStore) Ping(_ context.Context) error                { return nil }
func (s *integrationTestStore) Close() error                                { return nil }

// integrationTestSetup holds all test components.
type integrationTestSetup struct {
	server      *grpc.Server
	listener    net.Listener
	store       *integrationTestStore
	tokenStore  *mockstore.RunnerTokenStore
	tokenSvc    *auth.RunnerTokenService
	connManager *ConnectionManager
	runnerMgr   *core.RunnerManager
	router      *MessageRouter
	runnerSvc   *RunnerService
	client      pb.RunnerServiceClient
	clientConn  *grpc.ClientConn
}

// setupIntegrationTest creates a full integration test setup.
func setupIntegrationTest(t *testing.T) *integrationTestSetup {
	t.Helper()

	logger := zap.NewNop()

	// Create store
	testStore := newIntegrationTestStore()

	// Create token service
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })

	// Create connection manager
	connManager := NewConnectionManager(logger)

	// Create runner manager
	runnerMgr := core.NewRunnerManager(testStore, connManager, logger)

	// Create message router
	router := NewMessageRouter(logger, runnerMgr)

	// Create registry
	registry := core.NewRunnerRegistry(testStore, tokenSvc, logger)

	// Create runner service with all dependencies
	runnerSvc := NewRunnerService(logger,
		WithStore(testStore),
		WithTokenService(tokenSvc),
		WithConnectionManager(connManager),
		WithRunnerManager(runnerMgr),
		WithRouter(router),
		WithRegistry(registry),
	)

	// Create gRPC server
	s := grpc.NewServer()
	pb.RegisterRunnerServiceServer(s, runnerSvc)

	// Listen on a random port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// Start server in background
	go func() {
		_ = s.Serve(lis)
	}()

	// Create client connection
	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := pb.NewRunnerServiceClient(conn)

	return &integrationTestSetup{
		server:      s,
		listener:    lis,
		store:       testStore,
		tokenStore:  tokenStore,
		tokenSvc:    tokenSvc,
		connManager: connManager,
		runnerMgr:   runnerMgr,
		router:      router,
		runnerSvc:   runnerSvc,
		client:      client,
		clientConn:  conn,
	}
}

// cleanup shuts down the integration test setup.
func (s *integrationTestSetup) cleanup() {
	_ = s.clientConn.Close()
	s.server.GracefulStop()
}

// TestIntegration_Connect_FullFlow tests the complete Connect flow.
func TestIntegration_Connect_FullFlow(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Step 1: Create a token
	token, plaintext, err := setup.tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Step 2: Register a runner
	regResp, err := setup.client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Token:    plaintext,
		Name:     "test-runner",
		Hostname: "localhost",
	})
	require.NoError(t, err)
	require.True(t, regResp.Accepted)
	runnerID := regResp.RunnerId

	// Bind token to runner
	err = setup.tokenSvc.BindRunner(ctx, token.ID, runnerID)
	require.NoError(t, err)

	// Step 3: Connect with metadata
	md := metadata.New(map[string]string{
		"x-runner-id":    runnerID,
		"x-runner-token": plaintext,
	})
	connectCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := setup.client.Connect(connectCtx)
	require.NoError(t, err)

	// Step 4: Send a heartbeat
	err = stream.Send(&pb.RunnerMessage{
		Payload: &pb.RunnerMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				Status: "idle",
			},
		},
	})
	require.NoError(t, err)

	// Give time for server to process
	time.Sleep(100 * time.Millisecond)

	// Step 5: Verify runner is connected
	assert.True(t, setup.connManager.IsConnected(runnerID))
	assert.Equal(t, 1, setup.connManager.Count())

	// Step 6: Close stream
	err = stream.CloseSend()
	require.NoError(t, err)

	// Give time for server to process disconnect
	time.Sleep(100 * time.Millisecond)

	// Step 7: Verify runner is disconnected
	assert.False(t, setup.connManager.IsConnected(runnerID))
}

// TestIntegration_Connect_MissingRunnerID tests Connect with missing runner ID.
func TestIntegration_Connect_MissingRunnerID(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Connect without x-runner-id
	stream, err := setup.client.Connect(ctx)
	require.NoError(t, err)

	// Try to receive - should get error
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner credentials")
}

// TestIntegration_Connect_InvalidToken tests Connect with invalid token.
func TestIntegration_Connect_InvalidToken(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Create a runner first
	runner := &store.Runner{
		ID:       "run_test123",
		Name:     "test-runner",
		Hostname: "localhost",
		Status:   "offline",
	}
	err := setup.store.CreateRunner(ctx, runner)
	require.NoError(t, err)

	// Connect with invalid token
	md := metadata.New(map[string]string{
		"x-runner-id":    "run_test123",
		"x-runner-token": "invalid_token",
	})
	connectCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := setup.client.Connect(connectCtx)
	require.NoError(t, err)

	// Try to receive - should get authentication error
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

// TestIntegration_Connect_RunnerNotFound tests Connect with non-existent runner.
func TestIntegration_Connect_RunnerNotFound(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Create a token
	_, plaintext, err := setup.tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Connect with non-existent runner
	md := metadata.New(map[string]string{
		"x-runner-id":    "run_nonexistent",
		"x-runner-token": plaintext,
	})
	connectCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := setup.client.Connect(connectCtx)
	require.NoError(t, err)

	// Try to receive - should get not found error
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestIntegration_Connect_MultipleHeartbeats tests sending multiple heartbeats.
func TestIntegration_Connect_MultipleHeartbeats(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Setup runner
	token, plaintext, err := setup.tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	regResp, err := setup.client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Token:    plaintext,
		Name:     "test-runner",
		Hostname: "localhost",
	})
	require.NoError(t, err)
	runnerID := regResp.RunnerId

	err = setup.tokenSvc.BindRunner(ctx, token.ID, runnerID)
	require.NoError(t, err)

	// Connect
	md := metadata.New(map[string]string{
		"x-runner-id":    runnerID,
		"x-runner-token": plaintext,
	})
	connectCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := setup.client.Connect(connectCtx)
	require.NoError(t, err)

	// Send multiple heartbeats
	for i := 0; i < 5; i++ {
		err = stream.Send(&pb.RunnerMessage{
			Payload: &pb.RunnerMessage_Heartbeat{
				Heartbeat: &pb.Heartbeat{
					Status: "idle",
				},
			},
		})
		require.NoError(t, err)
	}

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	// Verify still connected
	assert.True(t, setup.connManager.IsConnected(runnerID))

	// Close
	err = stream.CloseSend()
	require.NoError(t, err)
}

// TestIntegration_Connect_SendTaskMessages tests sending task-related messages.
func TestIntegration_Connect_SendTaskMessages(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Setup runner
	token, plaintext, err := setup.tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	regResp, err := setup.client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Token:    plaintext,
		Name:     "test-runner",
		Hostname: "localhost",
	})
	require.NoError(t, err)
	runnerID := regResp.RunnerId

	err = setup.tokenSvc.BindRunner(ctx, token.ID, runnerID)
	require.NoError(t, err)

	// Connect
	md := metadata.New(map[string]string{
		"x-runner-id":    runnerID,
		"x-runner-token": plaintext,
	})
	connectCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := setup.client.Connect(connectCtx)
	require.NoError(t, err)

	// Send various task messages
	messages := []*pb.RunnerMessage{
		{
			Payload: &pb.RunnerMessage_TaskAccepted{
				TaskAccepted: &pb.TaskAccepted{
					TaskId: "task_123",
					RunId:  "trun_123",
				},
			},
		},
		{
			Payload: &pb.RunnerMessage_TaskStarted{
				TaskStarted: &pb.TaskStarted{
					TaskId: "task_123",
					RunId:  "trun_123",
				},
			},
		},
		{
			Payload: &pb.RunnerMessage_TaskProgress{
				TaskProgress: &pb.TaskProgress{
					TaskId:          "task_123",
					RunId:           "trun_123",
					ProgressPercent: 50,
				},
			},
		},
		{
			Payload: &pb.RunnerMessage_TaskCompleted{
				TaskCompleted: &pb.TaskCompleted{
					TaskId:  "task_123",
					RunId:   "trun_123",
					Success: true,
				},
			},
		},
	}

	for _, msg := range messages {
		err = stream.Send(msg)
		require.NoError(t, err)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify still connected
	assert.True(t, setup.connManager.IsConnected(runnerID))

	// Close
	err = stream.CloseSend()
	require.NoError(t, err)
}

// TestIntegration_StreamLogs tests the StreamLogs RPC.
func TestIntegration_StreamLogs(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Open log stream
	stream, err := setup.client.StreamLogs(ctx)
	require.NoError(t, err)

	// Send init message first
	err = stream.Send(&pb.StreamLogsMessage{
		Payload: &pb.StreamLogsMessage_Init{
			Init: &pb.StreamLogsInit{
				SessionId: "sess_123",
				TaskId:    "task_123",
				RunId:     "trun_123",
			},
		},
	})
	require.NoError(t, err)

	// Send some log entries
	for i := 0; i < 10; i++ {
		err = stream.Send(&pb.StreamLogsMessage{
			Payload: &pb.StreamLogsMessage_LogEntry{
				LogEntry: &pb.LogEntry{
					Content: fmt.Sprintf("log message %d", i),
				},
			},
		})
		require.NoError(t, err)
	}

	// Close and get response
	resp, err := stream.CloseAndRecv()
	require.NoError(t, err)
	assert.Equal(t, int64(10), resp.LogsReceived) // 10 log entries (init not counted)
}

// TestIntegration_RegisterRunner tests runner registration.
func TestIntegration_RegisterRunner(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Create a token
	_, plaintext, err := setup.tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Register new runner
	resp, err := setup.client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Token:        plaintext,
		Name:         "new-runner",
		Hostname:     "localhost",
		SandboxMode:  "runner-is-sandbox",
		SandboxTypes: []string{"docker"},
		Capabilities: []string{"arm64"},
		Labels:       map[string]string{"env": "test"},
	})
	require.NoError(t, err)
	require.True(t, resp.Accepted)
	assert.NotEmpty(t, resp.RunnerId)
	assert.Equal(t, "runner registered", resp.Message)

	// Register same runner again (re-register)
	resp2, err := setup.client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Token:    plaintext,
		Name:     "new-runner",
		Hostname: "localhost",
	})
	require.NoError(t, err)
	require.True(t, resp2.Accepted)
	assert.Equal(t, "runner re-registered", resp2.Message)
}

// TestIntegration_HandleDisconnect tests that handleDisconnect is called.
func TestIntegration_HandleDisconnect(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Setup and connect runner
	token, plaintext, err := setup.tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	regResp, err := setup.client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Token:    plaintext,
		Name:     "disconnect-test-runner",
		Hostname: "localhost",
	})
	require.NoError(t, err)
	runnerID := regResp.RunnerId

	err = setup.tokenSvc.BindRunner(ctx, token.ID, runnerID)
	require.NoError(t, err)

	// Connect
	md := metadata.New(map[string]string{
		"x-runner-id":    runnerID,
		"x-runner-token": plaintext,
	})
	connectCtx, cancel := context.WithCancel(metadata.NewOutgoingContext(ctx, md))
	defer cancel()

	stream, err := setup.client.Connect(connectCtx)
	require.NoError(t, err)

	// Send heartbeat to establish connection
	err = stream.Send(&pb.RunnerMessage{
		Payload: &pb.RunnerMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{Status: "idle"},
		},
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Verify connected
	assert.True(t, setup.connManager.IsConnected(runnerID))

	// Check runner status in store is idle
	runner, err := setup.store.GetRunner(ctx, runnerID)
	require.NoError(t, err)
	assert.Equal(t, "idle", runner.Status)

	// Cancel context to force disconnect
	cancel()

	// Give time for disconnect handling
	time.Sleep(200 * time.Millisecond)

	// Verify disconnected
	assert.False(t, setup.connManager.IsConnected(runnerID))

	// Check runner status in store is offline
	runner, err = setup.store.GetRunner(ctx, runnerID)
	require.NoError(t, err)
	assert.Equal(t, "offline", runner.Status)
}

// TestIntegration_SendCommand tests that commands can be sent to connected runner.
func TestIntegration_SendCommand(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Setup and connect runner
	token, plaintext, err := setup.tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	regResp, err := setup.client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Token:    plaintext,
		Name:     "command-test-runner",
		Hostname: "localhost",
	})
	require.NoError(t, err)
	runnerID := regResp.RunnerId

	err = setup.tokenSvc.BindRunner(ctx, token.ID, runnerID)
	require.NoError(t, err)

	// Connect
	md := metadata.New(map[string]string{
		"x-runner-id":    runnerID,
		"x-runner-token": plaintext,
	})
	connectCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := setup.client.Connect(connectCtx)
	require.NoError(t, err)

	// Send heartbeat
	err = stream.Send(&pb.RunnerMessage{
		Payload: &pb.RunnerMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{Status: "idle"},
		},
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Send a command via connection manager
	err = setup.connManager.SendCommand(runnerID, &pb.ServerCommand{
		Payload: &pb.ServerCommand_KillTask{
			KillTask: &pb.KillTask{
				TaskId: "task_123",
			},
		},
	})
	require.NoError(t, err)

	// Receive the command on the stream
	cmd, err := stream.Recv()
	require.NoError(t, err)
	assert.NotNil(t, cmd.GetKillTask())
	assert.Equal(t, "task_123", cmd.GetKillTask().GetTaskId())

	// Close
	err = stream.CloseSend()
	require.NoError(t, err)
}

// TestIntegration_Connect_ContextCancellation tests graceful disconnect on context cancellation.
func TestIntegration_Connect_ContextCancellation(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	ctx := context.Background()

	// Setup runner
	token, plaintext, err := setup.tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	regResp, err := setup.client.RegisterRunner(ctx, &pb.RegisterRunnerRequest{
		Token:    plaintext,
		Name:     "cancel-test-runner",
		Hostname: "localhost",
	})
	require.NoError(t, err)
	runnerID := regResp.RunnerId

	err = setup.tokenSvc.BindRunner(ctx, token.ID, runnerID)
	require.NoError(t, err)

	// Connect with cancelable context
	md := metadata.New(map[string]string{
		"x-runner-id":    runnerID,
		"x-runner-token": plaintext,
	})
	connectCtx, cancel := context.WithCancel(metadata.NewOutgoingContext(ctx, md))

	stream, err := setup.client.Connect(connectCtx)
	require.NoError(t, err)

	// Send heartbeat
	err = stream.Send(&pb.RunnerMessage{
		Payload: &pb.RunnerMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{Status: "idle"},
		},
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Verify connected
	assert.True(t, setup.connManager.IsConnected(runnerID))

	// Cancel context
	cancel()

	// Try to receive - should get error due to context cancellation
	_, err = stream.Recv()
	if err != nil && err != io.EOF {
		// Expected - context was cancelled
		assert.Error(t, err)
	}

	time.Sleep(200 * time.Millisecond)

	// Verify disconnected
	assert.False(t, setup.connManager.IsConnected(runnerID))
}
