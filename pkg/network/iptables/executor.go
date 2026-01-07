// Package iptables provides iptables rule generation and management for network isolation.
package iptables

import (
	"bytes"
	"context"
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
}

// NewMockExecutor creates a new mock executor for testing.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		Commands:     make([][]string, 0),
		IPv6Commands: make([][]string, 0),
		Outputs:      make(map[string][]byte),
		Errors:       make(map[string]error),
	}
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
}
