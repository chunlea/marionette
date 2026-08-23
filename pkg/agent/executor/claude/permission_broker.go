package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
)

// Real pre-execution permission gating for Claude Code.
//
// Verified against CLI 2.1.241: a `PreToolUse` hook supplied through
// `--settings` runs BEFORE the tool executes and can block it. The hook is a
// command; it receives a JSON object on stdin and answers on stdout with
//
//	{"hookSpecificOutput":{"hookEventName":"PreToolUse",
//	 "permissionDecision":"allow"|"deny","permissionDecisionReason":"..."}}
//
// On "deny" the tool never runs, the model receives an error tool_result
// carrying the reason, and the run's final result line records the denial
// under `permission_denials`.
//
// The hook is a separate process, so it talks back to the agent over a unix
// socket that lives for exactly one run. PermissionBroker is the agent side;
// RunPermissionHook is the hook side.
//
// IMPORTANT: when a hook exceeds its configured timeout the CLI FAILS OPEN and
// runs the tool anyway. The hook therefore always answers within its own,
// shorter deadline, denying if no decision arrived. Every failure path in this
// file denies.

const (
	hookEventPreToolUse = "PreToolUse"

	decisionAllow = "allow"
	decisionDeny  = "deny"

	// DefaultPermissionWait is how long a tool call may wait for an operator
	// decision before the hook denies it. It matches the server's default
	// suspend_after_seconds: past that point the session suspends and the task
	// is re-run after resume, so holding the CLI longer buys nothing.
	DefaultPermissionWait = 30 * time.Minute

	// hookTimeoutSlack is added to the wait when telling the CLI how long the
	// hook may take, so our own deny always wins the race against the CLI's
	// fail-open timeout.
	hookTimeoutSlack = 30 * time.Second
)

// hookInput is the payload the CLI writes to a PreToolUse hook's stdin.
type hookInput struct {
	HookEventName  string          `json:"hook_event_name"`
	SessionID      string          `json:"session_id"`
	CWD            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
	ToolName       string          `json:"tool_name"`
	ToolUseID      string          `json:"tool_use_id"`
	ToolInput      json.RawMessage `json:"tool_input"`
}

// hookOutput is what the CLI reads back from the hook's stdout.
type hookOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// brokerDecision is the broker's reply to the hook process.
type brokerDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// PermissionBroker answers permission questions from hook subprocesses over a
// unix socket for the lifetime of one run.
type PermissionBroker struct {
	socketPath string
	dir        string
	listener   net.Listener
	handler    executor.OutputHandler
	wait       time.Duration

	wg      sync.WaitGroup
	closeMu sync.Mutex
	closed  bool
}

// NewPermissionBroker creates a broker listening on a private unix socket.
// Decisions are delegated to handler.HandlePermissionRequest.
func NewPermissionBroker(handler executor.OutputHandler, wait time.Duration) (*PermissionBroker, error) {
	if handler == nil {
		return nil, errors.New("permission broker requires an output handler")
	}
	if wait <= 0 {
		wait = DefaultPermissionWait
	}

	dir, err := os.MkdirTemp(socketBaseDir(), "mrn-perm-")
	if err != nil {
		return nil, fmt.Errorf("failed to create permission socket dir: %w", err)
	}

	socketPath := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("failed to listen on permission socket: %w", err)
	}

	return &PermissionBroker{
		socketPath: socketPath,
		dir:        dir,
		listener:   listener,
		handler:    handler,
		wait:       wait,
	}, nil
}

// socketBaseDir picks a directory short enough for a unix socket path. The
// sockaddr_un limit is ~104 bytes on macOS and TMPDIR there is long enough to
// blow it.
func socketBaseDir() string {
	base := os.TempDir()
	// "<base>/mrn-perm-XXXXXXXXXX/s" must stay under the limit.
	if len(base)+24 > 100 {
		return "/tmp"
	}
	return base
}

// SocketPath returns the socket the hook subprocess must connect to.
func (b *PermissionBroker) SocketPath() string { return b.socketPath }

// Wait returns the per-request decision budget.
func (b *PermissionBroker) Wait() time.Duration { return b.wait }

// Serve accepts hook connections until the broker is closed. It returns
// immediately; connections are handled in the background.
func (b *PermissionBroker) Serve(ctx context.Context) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			conn, err := b.listener.Accept()
			if err != nil {
				return // listener closed
			}
			b.wg.Add(1)
			go func() {
				defer b.wg.Done()
				defer func() { _ = conn.Close() }()
				b.handleConn(ctx, conn)
			}()
		}
	}()
}

// handleConn answers one hook process. Every error path denies.
func (b *PermissionBroker) handleConn(ctx context.Context, conn net.Conn) {
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}

	decision := b.decide(ctx, line)

	payload, err := json.Marshal(decision)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(payload, '\n'))
}

// decide turns a hook payload into an allow/deny decision.
func (b *PermissionBroker) decide(ctx context.Context, payload []byte) brokerDecision {
	var input hookInput
	if err := json.Unmarshal(payload, &input); err != nil {
		return brokerDecision{Decision: decisionDeny, Reason: "marionette: unreadable permission request"}
	}

	if !RequiresPermission(input.ToolName) {
		return brokerDecision{Decision: decisionAllow, Reason: "marionette: tool is read-only"}
	}

	req := &executor.PermissionRequest{
		ID:        input.ToolUseID,
		Tool:      input.ToolName,
		Action:    string(input.ToolInput),
		Context:   input.CWD,
		RiskLevel: RiskLevelFor(input.ToolName),
	}

	// Bounded wait: exceeding it must deny, because the CLI would otherwise
	// hit its own hook timeout and run the tool unapproved.
	ctx, cancel := context.WithTimeout(ctx, b.wait)
	defer cancel()

	approved, err := b.handler.HandlePermissionRequest(ctx, req)
	switch {
	case err != nil:
		return brokerDecision{
			Decision: decisionDeny,
			Reason:   "marionette: no approval received (" + err.Error() + ")",
		}
	case !approved:
		return brokerDecision{Decision: decisionDeny, Reason: "marionette: denied by operator"}
	default:
		return brokerDecision{Decision: decisionAllow, Reason: "marionette: approved by operator"}
	}
}

// Close stops the broker and removes the socket.
func (b *PermissionBroker) Close() error {
	b.closeMu.Lock()
	if b.closed {
		b.closeMu.Unlock()
		return nil
	}
	b.closed = true
	b.closeMu.Unlock()

	err := b.listener.Close()
	b.wg.Wait()
	_ = os.RemoveAll(b.dir)
	return err
}

// hookSettings renders the --settings payload that installs the PreToolUse
// hook. The matcher is "*" so every tool call is seen; the broker decides
// which ones actually need an operator.
func hookSettings(argv []string, timeout time.Duration) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("permission hook command is empty")
	}

	type hookEntry struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	type matcherEntry struct {
		Matcher string      `json:"matcher"`
		Hooks   []hookEntry `json:"hooks"`
	}
	type settings struct {
		Hooks map[string][]matcherEntry `json:"hooks"`
	}

	seconds := int((timeout + hookTimeoutSlack).Seconds())
	if seconds < 1 {
		seconds = 1
	}

	payload, err := json.Marshal(settings{
		Hooks: map[string][]matcherEntry{
			hookEventPreToolUse: {{
				Matcher: "*",
				Hooks: []hookEntry{{
					Type:    "command",
					Command: shellQuoteArgv(argv),
					Timeout: seconds,
				}},
			}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode hook settings: %w", err)
	}
	return string(payload), nil
}

// shellQuoteArgv renders argv as a single shell command line, since the hook
// `command` field is executed by a shell.
func shellQuoteArgv(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", `'\''`)+"'")
	}
	return strings.Join(quoted, " ")
}

// RunPermissionHook is the hook side of the gate: it forwards the CLI's
// PreToolUse payload to the broker and prints the CLI's expected decision.
//
// It never returns without writing a decision, and every failure denies. A
// hook that dies silently would let the CLI fail open and run the tool.
func RunPermissionHook(ctx context.Context, socketPath string, in io.Reader, out io.Writer) error {
	decision := brokerDecision{
		Decision: decisionDeny,
		Reason:   "marionette: permission gate unavailable",
	}

	if payload, err := io.ReadAll(io.LimitReader(in, 4*1024*1024)); err == nil {
		if got, err := askBroker(ctx, socketPath, payload); err == nil {
			decision = got
		} else {
			decision.Reason = "marionette: permission gate unreachable (" + err.Error() + ")"
		}
	}

	if decision.Decision != decisionAllow {
		decision.Decision = decisionDeny
	}

	return json.NewEncoder(out).Encode(hookOutput{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:            hookEventPreToolUse,
			PermissionDecision:       decision.Decision,
			PermissionDecisionReason: decision.Reason,
		},
	})
}

// askBroker performs the socket round trip.
func askBroker(ctx context.Context, socketPath string, payload []byte) (brokerDecision, error) {
	var decision brokerDecision

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return decision, err
	}
	defer func() { _ = conn.Close() }()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	payload = append(bytesTrimNewline(payload), '\n')
	if _, err := conn.Write(payload); err != nil {
		return decision, err
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return decision, err
	}

	if err := json.Unmarshal(bytesTrimNewline(line), &decision); err != nil {
		return decision, err
	}
	return decision, nil
}

// bytesTrimNewline strips trailing newlines so the payload stays one line.
func bytesTrimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// PermissionHookCommand is the argv[1] the agent binary recognises as "run as
// the PreToolUse permission hook". The executor re-invokes its own binary with
// this subcommand plus the broker's socket path.
const PermissionHookCommand = "permission-hook"
