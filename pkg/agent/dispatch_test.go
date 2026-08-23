package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// orderRecorder records the sequence in which commands were handled.
type orderRecorder struct {
	mu    sync.Mutex
	order []string
	seen  chan string
}

func newOrderRecorder() *orderRecorder {
	return &orderRecorder{seen: make(chan string, 32)}
}

func (r *orderRecorder) record(event string) {
	r.mu.Lock()
	r.order = append(r.order, event)
	r.mu.Unlock()

	select {
	case r.seen <- event:
	default:
	}
}

func (r *orderRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

// waitFor blocks until event has been recorded.
func (r *orderRecorder) waitFor(t *testing.T, event string, timeout time.Duration) {
	t.Helper()

	require.Eventually(t, func() bool {
		for _, got := range r.snapshot() {
			if got == event {
				return true
			}
		}
		return false
	}, timeout, 10*time.Millisecond, "never observed %q; got %v", event, r.snapshot())
}

// startOrderingChannel wires a control channel to a mock server that pushes
// the given commands down the stream, in order, as fast as it can.
func startOrderingChannel(t *testing.T, handler CommandHandler, commands []*pb.ServerCommand) *ControlChannel {
	t.Helper()

	server, err := NewMockServer()
	require.NoError(t, err)

	server.ConnectFunc = func(stream pb.RunnerService_ConnectServer) error {
		for _, cmd := range commands {
			if err := stream.Send(cmd); err != nil {
				return nil //nolint:nilerr // intentional: client disconnect is not a server error
			}
		}
		for {
			if _, err := stream.Recv(); err != nil {
				return nil //nolint:nilerr // intentional: client disconnect is not a server error
			}
		}
	}
	server.Start()
	t.Cleanup(server.Stop)

	logger := zaptest.NewLogger(t)
	client := NewClient(testClientConfig(server.Addr()), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() { _ = client.Close() })

	cc := NewControlChannel(client, handler, logger)
	require.NoError(t, cc.Start(ctx))
	t.Cleanup(cc.Stop)

	return cc
}

// TestControlChannel_ApprovePermissionCannotOvertakeItsTask is the regression
// test for ordered delivery. receiveLoop used to spawn a goroutine per command,
// so ApprovePermission and KillTask raced the ExecuteTask they belong to and
// could be handled before the task had registered - the response then had
// nothing to deliver to.
//
// The fake task deliberately takes its time before reporting itself accepted.
// Under the old goroutine-per-command behavior the later commands landed inside
// that window; under ordered delivery they cannot.
func TestControlChannel_ApprovePermissionCannotOvertakeItsTask(t *testing.T) {
	const registrationDelay = 250 * time.Millisecond

	recorder := newOrderRecorder()
	release := make(chan struct{})

	logger := zaptest.NewLogger(t)
	handler := NewDefaultCommandHandler(NewWorkspaceManager(t.TempDir(), logger), logger)

	// Attach the session up front so the task is not rejected.
	_, err := handler.HandleAttachSession(context.Background(), &pb.AttachSession{
		SessionId:     "sess_order",
		WorkspacePath: "ws_order",
		AgentConfig:   &pb.AgentConfig{Agent: "claude"},
	})
	require.NoError(t, err)

	handler.OnExecuteTask = func(ctx context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error) {
		recorder.record("execute:start")

		// Stand in for the work a real runner does before it is ready to
		// receive commands about this task.
		time.Sleep(registrationDelay)

		recorder.record("execute:accepted")
		TaskAccepted(ctx)

		// Block, the way a real task does for its whole duration.
		<-release
		recorder.record("execute:end")

		return &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_TaskCompleted{
				TaskCompleted: &pb.TaskCompleted{TaskId: cmd.TaskId, Success: true},
			},
		}, nil
	}
	handler.OnApprovePermission = func(context.Context, *pb.ApprovePermission) error {
		recorder.record("approve")
		return nil
	}
	handler.OnKillTask = func(context.Context, *pb.KillTask) error {
		recorder.record("kill")
		return nil
	}

	startOrderingChannel(t, handler, []*pb.ServerCommand{
		{Payload: &pb.ServerCommand_ExecuteTask{ExecuteTask: &pb.ExecuteTask{
			TaskId: "task_order", RunId: "trun_order", SessionId: "sess_order", Attempt: 1, Prompt: "hi",
		}}},
		{Payload: &pb.ServerCommand_ApprovePermission{ApprovePermission: &pb.ApprovePermission{
			RequestId: "perm_order", TaskId: "task_order", Approved: true, Tool: "Bash",
		}}},
		{Payload: &pb.ServerCommand_KillTask{KillTask: &pb.KillTask{TaskId: "task_order"}}},
	})

	// Both follow-up commands must arrive even though the task is still
	// running: holding the queue for the task's duration would deadlock them.
	recorder.waitFor(t, "approve", 10*time.Second)
	recorder.waitFor(t, "kill", 10*time.Second)

	order := recorder.snapshot()
	assert.Equal(t, []string{"execute:start", "execute:accepted", "approve", "kill"}, order,
		"commands must be delivered in the order the server sent them")

	close(release)
	recorder.waitFor(t, "execute:end", 10*time.Second)
}

// TestControlChannel_AttachBeforeExecute covers the other ordering hazard the
// goroutine-per-command dispatch created: an ExecuteTask handled before the
// AttachSession that precedes it is rejected outright as "session not
// attached".
func TestControlChannel_AttachBeforeExecute(t *testing.T) {
	recorder := newOrderRecorder()
	done := make(chan *pb.RunnerMessage, 1)

	logger := zaptest.NewLogger(t)
	handler := NewDefaultCommandHandler(NewWorkspaceManager(t.TempDir(), logger), logger)

	handler.OnExecuteTask = func(ctx context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error) {
		TaskAccepted(ctx)
		recorder.record("execute")

		msg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_TaskCompleted{
				TaskCompleted: &pb.TaskCompleted{TaskId: cmd.TaskId, Success: true},
			},
		}
		done <- msg
		return msg, nil
	}

	startOrderingChannel(t, handler, []*pb.ServerCommand{
		{Payload: &pb.ServerCommand_AttachSession{AttachSession: &pb.AttachSession{
			SessionId:     "sess_attach",
			WorkspacePath: "ws_attach",
			AgentConfig:   &pb.AgentConfig{Agent: "claude"},
		}}},
		{Payload: &pb.ServerCommand_ExecuteTask{ExecuteTask: &pb.ExecuteTask{
			TaskId: "task_attach", RunId: "trun_attach", SessionId: "sess_attach", Attempt: 1, Prompt: "hi",
		}}},
	})

	select {
	case msg := <-done:
		completed := msg.GetTaskCompleted()
		require.NotNil(t, completed)
		assert.True(t, completed.Success)
	case <-time.After(10 * time.Second):
		t.Fatalf("task never ran; recorded %v", recorder.snapshot())
	}
}

// TestControlChannel_RejectedTaskDoesNotStallTheQueue covers the handoff's
// escape hatch: a handler that returns without ever reporting the task
// accepted must not hold the queue.
func TestControlChannel_RejectedTaskDoesNotStallTheQueue(t *testing.T) {
	recorder := newOrderRecorder()

	logger := zaptest.NewLogger(t)
	handler := NewDefaultCommandHandler(NewWorkspaceManager(t.TempDir(), logger), logger)

	// No session attached, so HandleExecuteTask rejects before it ever reaches
	// OnExecuteTask and TaskAccepted is never called.
	handler.OnApprovePermission = func(context.Context, *pb.ApprovePermission) error {
		recorder.record("approve")
		return nil
	}

	startOrderingChannel(t, handler, []*pb.ServerCommand{
		{Payload: &pb.ServerCommand_ExecuteTask{ExecuteTask: &pb.ExecuteTask{
			TaskId: "task_reject", RunId: "trun_reject", SessionId: "sess_missing", Attempt: 1, Prompt: "hi",
		}}},
		{Payload: &pb.ServerCommand_ApprovePermission{ApprovePermission: &pb.ApprovePermission{
			RequestId: "perm_reject", TaskId: "task_reject", Approved: true, Tool: "Bash",
		}}},
	})

	recorder.waitFor(t, "approve", 10*time.Second)
}

// TestTaskAccepted_NoopOutsideDispatcher keeps handlers free to call it
// unconditionally.
func TestTaskAccepted_NoopOutsideDispatcher(t *testing.T) {
	assert.NotPanics(t, func() {
		TaskAccepted(context.Background())
	})
}
