// Package iptables provides iptables rule generation and management for network isolation.
package iptables

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Executor is an interface for executing iptables commands.
// This allows mocking the command execution for testing.
type Executor interface {
	// Run executes an iptables command and returns an error if it fails.
	Run(ctx context.Context, args ...string) error

	// Output executes an iptables command and returns its output.
	Output(ctx context.Context, args ...string) ([]byte, error)

	// RunIPv6 executes an ip6tables command and returns an error if it fails.
	RunIPv6(ctx context.Context, args ...string) error

	// OutputIPv6 executes an ip6tables command and returns its output.
	OutputIPv6(ctx context.Context, args ...string) ([]byte, error)
}

// RealExecutor executes actual iptables commands on the system.
// Requires root privileges to function.
type RealExecutor struct {
	// iptablesPath is the path to the iptables binary.
	iptablesPath string

	// ip6tablesPath is the path to the ip6tables binary.
	ip6tablesPath string
}

// NewRealExecutor creates a new executor that runs real iptables commands.
func NewRealExecutor() *RealExecutor {
	return &RealExecutor{
		iptablesPath:  "iptables",
		ip6tablesPath: "ip6tables",
	}
}

// Run executes an iptables command.
func (e *RealExecutor) Run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, e.iptablesPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("iptables %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

// Output executes an iptables command and returns its output.
func (e *RealExecutor) Output(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.iptablesPath, args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("iptables %s: %w: %s", strings.Join(args, " "), err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("iptables %s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

// RunIPv6 executes an ip6tables command.
func (e *RealExecutor) RunIPv6(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, e.ip6tablesPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ip6tables %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

// OutputIPv6 executes an ip6tables command and returns its output.
func (e *RealExecutor) OutputIPv6(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.ip6tablesPath, args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("ip6tables %s: %w: %s", strings.Join(args, " "), err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("ip6tables %s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

// MockExecutor records commands for testing instead of executing them.
type MockExecutor struct {
	mu sync.Mutex

	// Commands records all iptables commands executed.
	Commands [][]string

	// IPv6Commands records all ip6tables commands executed.
	IPv6Commands [][]string

	// Outputs maps command strings to their mock outputs.
	Outputs map[string][]byte

	// Errors maps command strings to their mock errors.
	Errors map[string]error

	// CheckErr is returned for -C (rule exists?) probes that are not listed in
	// checkOK. It defaults to iptables' "Bad rule" complaint so a fresh mock
	// behaves like an empty ruleset: without it every conditional insert would
	// believe its rule was already present and silently do nothing.
	CheckErr error

	// checkOK marks -C probes that should succeed.
	checkOK map[string]bool
}

// ErrMockRuleMissing is what iptables prints when -C or -D finds no such rule.
var ErrMockRuleMissing = errors.New("iptables: Bad rule (does a matching rule exist in that chain?).")

// NewMockExecutor creates a new mock executor for testing.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		Commands:     make([][]string, 0),
		IPv6Commands: make([][]string, 0),
		Outputs:      make(map[string][]byte),
		Errors:       make(map[string]error),
		CheckErr:     ErrMockRuleMissing,
		checkOK:      make(map[string]bool),
	}
}

// SetCheckOK makes the -C probe for the given rule arguments succeed, i.e.
// pretend the rule is already installed.
func (m *MockExecutor) SetCheckOK(args []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkOK[strings.Join(args, " ")] = true
}

// SetIPv6CheckOK is SetCheckOK for the ip6tables side.
func (m *MockExecutor) SetIPv6CheckOK(args []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkOK["v6:"+strings.Join(args, " ")] = true
}

// checkResult resolves a -C probe. Caller holds the lock.
func (m *MockExecutor) checkResult(args []string, key string) (error, bool) {
	if len(args) == 0 || args[0] != "-C" {
		return nil, false
	}
	if m.checkOK[key] {
		return nil, true
	}
	return m.CheckErr, true
}

// Run records the command and returns any configured error.
func (m *MockExecutor) Run(ctx context.Context, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Commands = append(m.Commands, args)

	key := strings.Join(args, " ")
	if err, ok := m.Errors[key]; ok {
		return err
	}
	if err, handled := m.checkResult(args, key); handled {
		return err
	}
	return nil
}

// Output records the command and returns any configured output or error.
func (m *MockExecutor) Output(ctx context.Context, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Commands = append(m.Commands, args)

	key := strings.Join(args, " ")
	if err, ok := m.Errors[key]; ok {
		return nil, err
	}
	if output, ok := m.Outputs[key]; ok {
		return output, nil
	}
	return []byte{}, nil
}

// RunIPv6 records the IPv6 command and returns any configured error.
func (m *MockExecutor) RunIPv6(ctx context.Context, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.IPv6Commands = append(m.IPv6Commands, args)

	key := "v6:" + strings.Join(args, " ")
	if err, ok := m.Errors[key]; ok {
		return err
	}
	if err, handled := m.checkResult(args, key); handled {
		return err
	}
	return nil
}

// OutputIPv6 records the IPv6 command and returns any configured output or error.
func (m *MockExecutor) OutputIPv6(ctx context.Context, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.IPv6Commands = append(m.IPv6Commands, args)

	key := "v6:" + strings.Join(args, " ")
	if err, ok := m.Errors[key]; ok {
		return nil, err
	}
	if output, ok := m.Outputs[key]; ok {
		return output, nil
	}
	return []byte{}, nil
}

// SetOutput configures a mock output for a command.
func (m *MockExecutor) SetOutput(args []string, output []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Outputs[strings.Join(args, " ")] = output
}

// SetError configures a mock error for a command.
func (m *MockExecutor) SetError(args []string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Errors[strings.Join(args, " ")] = err
}

// GetCommands returns a copy of all recorded commands.
func (m *MockExecutor) GetCommands() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]string, len(m.Commands))
	copy(result, m.Commands)
	return result
}

// GetIPv6Commands returns a copy of all recorded IPv6 commands.
func (m *MockExecutor) GetIPv6Commands() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]string, len(m.IPv6Commands))
	copy(result, m.IPv6Commands)
	return result
}

// Reset clears all recorded commands and configured outputs/errors.
func (m *MockExecutor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Commands = make([][]string, 0)
	m.IPv6Commands = make([][]string, 0)
	m.Outputs = make(map[string][]byte)
	m.Errors = make(map[string]error)
	m.checkOK = make(map[string]bool)
}

// NamespaceExecutor runs iptables inside another network namespace.
//
// The host's iptables binary is entered into the target namespace with
// nsenter, rather than exec'd inside the container: a runner image is not
// required to ship iptables, and the sandbox must never be able to see or
// edit the rules that constrain it.
//
// Requires CAP_NET_ADMIN in the target namespace, which in practice means the
// server process runs as root on the Docker host.
type NamespaceExecutor struct {
	// resolve returns the current network namespace path. It is called for
	// every command, so a container that restarted under a new PID is picked
	// up without recreating the executor.
	resolve func(ctx context.Context) (string, error)

	nsenterPath   string
	iptablesPath  string
	ip6tablesPath string
}

// NewNamespaceExecutor creates an executor targeting the namespace returned by
// resolve.
func NewNamespaceExecutor(resolve func(ctx context.Context) (string, error)) *NamespaceExecutor {
	return &NamespaceExecutor{
		resolve:       resolve,
		nsenterPath:   "nsenter",
		iptablesPath:  "iptables",
		ip6tablesPath: "ip6tables",
	}
}

// Run executes an iptables command inside the namespace.
func (e *NamespaceExecutor) Run(ctx context.Context, args ...string) error {
	_, err := e.run(ctx, e.iptablesPath, args)
	return err
}

// Output executes an iptables command inside the namespace and returns stdout.
func (e *NamespaceExecutor) Output(ctx context.Context, args ...string) ([]byte, error) {
	return e.run(ctx, e.iptablesPath, args)
}

// RunIPv6 executes an ip6tables command inside the namespace.
func (e *NamespaceExecutor) RunIPv6(ctx context.Context, args ...string) error {
	_, err := e.run(ctx, e.ip6tablesPath, args)
	return err
}

// OutputIPv6 executes an ip6tables command inside the namespace.
func (e *NamespaceExecutor) OutputIPv6(ctx context.Context, args ...string) ([]byte, error) {
	return e.run(ctx, e.ip6tablesPath, args)
}

func (e *NamespaceExecutor) run(ctx context.Context, binary string, args []string) ([]byte, error) {
	nsPath, err := e.resolve(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving network namespace: %w", err)
	}

	full := append([]string{"--net=" + nsPath, "--", binary}, args...)
	cmd := exec.CommandContext(ctx, e.nsenterPath, full...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// iptables reports "No chain..." and "Bad rule..." on stderr; callers
		// match on those strings, so they have to survive into the error.
		return nil, fmt.Errorf("%s %s in %s: %w: %s",
			binary, strings.Join(args, " "), nsPath, err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

// Ensure NamespaceExecutor satisfies the Executor interface.
var _ Executor = (*NamespaceExecutor)(nil)
