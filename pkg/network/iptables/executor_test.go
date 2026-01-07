package iptables

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMockExecutor(t *testing.T) {
	m := NewMockExecutor()
	assert.NotNil(t, m)
	assert.Empty(t, m.Commands)
	assert.Empty(t, m.IPv6Commands)
}

func TestMockExecutor_Run(t *testing.T) {
	t.Run("records commands", func(t *testing.T) {
		m := NewMockExecutor()

		err := m.Run(context.Background(), "-A", "OUTPUT", "-j", "DROP")
		require.NoError(t, err)

		commands := m.GetCommands()
		require.Len(t, commands, 1)
		assert.Equal(t, []string{"-A", "OUTPUT", "-j", "DROP"}, commands[0])
	})

	t.Run("returns configured error", func(t *testing.T) {
		m := NewMockExecutor()
		m.SetError([]string{"-A", "OUTPUT"}, errors.New("permission denied"))

		err := m.Run(context.Background(), "-A", "OUTPUT")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("success without error", func(t *testing.T) {
		m := NewMockExecutor()

		err := m.Run(context.Background(), "-N", "TEST_CHAIN")
		require.NoError(t, err)
	})
}

func TestMockExecutor_Output(t *testing.T) {
	t.Run("returns configured output", func(t *testing.T) {
		m := NewMockExecutor()
		expectedOutput := []byte("Chain OUTPUT (policy ACCEPT)")
		m.SetOutput([]string{"-L", "OUTPUT"}, expectedOutput)

		output, err := m.Output(context.Background(), "-L", "OUTPUT")
		require.NoError(t, err)
		assert.Equal(t, expectedOutput, output)
	})

	t.Run("returns empty for unconfigured", func(t *testing.T) {
		m := NewMockExecutor()

		output, err := m.Output(context.Background(), "-L", "UNKNOWN")
		require.NoError(t, err)
		assert.Empty(t, output)
	})
}

func TestMockExecutor_RunIPv6(t *testing.T) {
	t.Run("records IPv6 commands", func(t *testing.T) {
		m := NewMockExecutor()

		err := m.RunIPv6(context.Background(), "-A", "OUTPUT", "-j", "DROP")
		require.NoError(t, err)

		commands := m.GetIPv6Commands()
		require.Len(t, commands, 1)
		assert.Equal(t, []string{"-A", "OUTPUT", "-j", "DROP"}, commands[0])
	})

	t.Run("separates IPv4 and IPv6 commands", func(t *testing.T) {
		m := NewMockExecutor()

		_ = m.Run(context.Background(), "-A", "OUTPUT", "-j", "ACCEPT")
		_ = m.RunIPv6(context.Background(), "-A", "OUTPUT", "-j", "DROP")

		assert.Len(t, m.GetCommands(), 1)
		assert.Len(t, m.GetIPv6Commands(), 1)
	})
}

func TestMockExecutor_OutputIPv6(t *testing.T) {
	t.Run("returns configured output", func(t *testing.T) {
		m := NewMockExecutor()
		expectedOutput := []byte("Chain OUTPUT (policy ACCEPT)")
		m.Outputs["v6:-L OUTPUT"] = expectedOutput

		output, err := m.OutputIPv6(context.Background(), "-L", "OUTPUT")
		require.NoError(t, err)
		assert.Equal(t, expectedOutput, output)
	})

	t.Run("returns error for configured error", func(t *testing.T) {
		m := NewMockExecutor()
		m.Errors["v6:-L OUTPUT"] = errors.New("permission denied")

		output, err := m.OutputIPv6(context.Background(), "-L", "OUTPUT")
		require.Error(t, err)
		assert.Nil(t, output)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("returns empty for unconfigured", func(t *testing.T) {
		m := NewMockExecutor()

		output, err := m.OutputIPv6(context.Background(), "-L", "UNKNOWN")
		require.NoError(t, err)
		assert.Empty(t, output)
	})

	t.Run("records IPv6 commands", func(t *testing.T) {
		m := NewMockExecutor()

		_, _ = m.OutputIPv6(context.Background(), "-L", "OUTPUT", "-n")

		commands := m.GetIPv6Commands()
		require.Len(t, commands, 1)
		assert.Equal(t, []string{"-L", "OUTPUT", "-n"}, commands[0])
	})
}

func TestMockExecutor_Reset(t *testing.T) {
	m := NewMockExecutor()

	_ = m.Run(context.Background(), "-A", "OUTPUT")
	_ = m.RunIPv6(context.Background(), "-A", "OUTPUT")
	m.SetOutput([]string{"-L"}, []byte("test"))
	m.SetError([]string{"-X"}, errors.New("error"))

	assert.NotEmpty(t, m.GetCommands())
	assert.NotEmpty(t, m.GetIPv6Commands())

	m.Reset()

	assert.Empty(t, m.GetCommands())
	assert.Empty(t, m.GetIPv6Commands())
	assert.Empty(t, m.Outputs)
	assert.Empty(t, m.Errors)
}

func TestMockExecutor_Concurrency(t *testing.T) {
	m := NewMockExecutor()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			_ = m.Run(context.Background(), "-A", "OUTPUT")
			_ = m.RunIPv6(context.Background(), "-A", "OUTPUT")
			_ = m.GetCommands()
			_ = m.GetIPv6Commands()
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have recorded 10 of each type
	assert.Len(t, m.GetCommands(), 10)
	assert.Len(t, m.GetIPv6Commands(), 10)
}

func TestNewRealExecutor(t *testing.T) {
	e := NewRealExecutor()
	assert.NotNil(t, e)
	assert.Equal(t, "iptables", e.iptablesPath)
	assert.Equal(t, "ip6tables", e.ip6tablesPath)
}
