package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockParser is a test parser implementation.
type MockParser struct {
	events   []*AgentEvent
	flushed  bool
	resetted bool
}

func NewMockParser() AgentEventParser {
	return &MockParser{}
}

func (p *MockParser) ParseLine(line []byte) ([]*AgentEvent, error) {
	if len(line) == 0 {
		return nil, nil
	}
	event := NewTextEvent(string(line))
	p.events = append(p.events, event)
	return []*AgentEvent{event}, nil
}

func (p *MockParser) Flush() ([]*AgentEvent, error) {
	p.flushed = true
	return nil, nil
}

func (p *MockParser) Reset() {
	p.events = nil
	p.flushed = false
	p.resetted = true
}

func TestParserRegistry_Register(t *testing.T) {
	registry := NewParserRegistry()

	registry.Register("mock", NewMockParser)

	assert.True(t, registry.Has("mock"))
	assert.False(t, registry.Has("unknown"))
}

func TestParserRegistry_Get(t *testing.T) {
	registry := NewParserRegistry()
	registry.Register("mock", NewMockParser)

	t.Run("registered parser", func(t *testing.T) {
		parser, err := registry.Get("mock")
		require.NoError(t, err)
		assert.NotNil(t, parser)

		// Verify it's a new instance each time
		parser2, err := registry.Get("mock")
		require.NoError(t, err)
		assert.NotSame(t, parser, parser2)
	})

	t.Run("unknown parser", func(t *testing.T) {
		parser, err := registry.Get("unknown")
		assert.ErrorIs(t, err, ErrUnknownAgent)
		assert.Nil(t, parser)
	})
}

func TestParserRegistry_Has(t *testing.T) {
	registry := NewParserRegistry()

	assert.False(t, registry.Has("mock"))

	registry.Register("mock", NewMockParser)

	assert.True(t, registry.Has("mock"))
}

func TestParserRegistry_Agents(t *testing.T) {
	registry := NewParserRegistry()

	assert.Empty(t, registry.Agents())

	registry.Register("claude", NewMockParser)
	registry.Register("codex", NewMockParser)

	agents := registry.Agents()
	assert.Len(t, agents, 2)
	assert.Contains(t, agents, "claude")
	assert.Contains(t, agents, "codex")
}

func TestMockParser_ParseLine(t *testing.T) {
	parser := NewMockParser().(*MockParser)

	t.Run("empty line", func(t *testing.T) {
		events, err := parser.ParseLine([]byte{})
		assert.NoError(t, err)
		assert.Nil(t, events)
	})

	t.Run("non-empty line", func(t *testing.T) {
		events, err := parser.ParseLine([]byte("hello world"))
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, EventText, events[0].Type)
		assert.Equal(t, "hello world", events[0].Text)
	})
}

func TestMockParser_Flush(t *testing.T) {
	parser := NewMockParser().(*MockParser)

	events, err := parser.Flush()
	assert.NoError(t, err)
	assert.Nil(t, events)
	assert.True(t, parser.flushed)
}

func TestMockParser_Reset(t *testing.T) {
	parser := NewMockParser().(*MockParser)

	// Add some state
	_, _ = parser.ParseLine([]byte("test"))
	assert.NotEmpty(t, parser.events)

	// Reset
	parser.Reset()
	assert.Empty(t, parser.events)
	assert.True(t, parser.resetted)
}

func TestDefaultRegistry(t *testing.T) {
	// Save original state
	originalRegistry := DefaultRegistry
	DefaultRegistry = NewParserRegistry()
	defer func() { DefaultRegistry = originalRegistry }()

	// Test RegisterParser
	RegisterParser("test-agent", NewMockParser)
	assert.True(t, DefaultRegistry.Has("test-agent"))

	// Test GetParser
	parser, err := GetParser("test-agent")
	require.NoError(t, err)
	assert.NotNil(t, parser)

	// Test unknown
	parser, err = GetParser("unknown")
	assert.ErrorIs(t, err, ErrUnknownAgent)
	assert.Nil(t, parser)
}

func TestParserRegistry_Concurrent(t *testing.T) {
	registry := NewParserRegistry()

	// Register concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			registry.Register("agent"+string(rune('0'+n)), NewMockParser)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have 10 agents registered
	agents := registry.Agents()
	assert.Len(t, agents, 10)
}
