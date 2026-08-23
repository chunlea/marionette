package agent

import "context"

// taskAcceptedKey is the context key carrying the dispatcher's handoff signal.
type taskAcceptedKey struct{}

// withTaskAccepted returns a context carrying a one-shot signal the command
// dispatcher waits on before delivering the next command.
func withTaskAccepted(ctx context.Context, signal func()) context.Context {
	return context.WithValue(ctx, taskAcceptedKey{}, signal)
}

// TaskAccepted reports that the task carried by ctx is registered, so commands
// referring to it - ApprovePermission, KillTask - can now be delivered.
//
// A handler whose HandleExecuteTask blocks for the whole duration of the task
// must call this as soon as the task is registered. Until it does, the command
// dispatcher holds the queue, which is what stops a later command from
// overtaking the task it belongs to. Failing to call it is not fatal: the
// dispatcher also resumes when the handler returns, so the worst case is that
// the queue is held for the length of the task.
//
// It is a no-op for a context that did not come from the dispatcher, so
// handlers can call it unconditionally.
func TaskAccepted(ctx context.Context) {
	if signal, ok := ctx.Value(taskAcceptedKey{}).(func()); ok {
		signal()
	}
}
