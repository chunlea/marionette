package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockParser is a mock implementation of AgentEventParser for testing.
type MockParser struct {
	agentType string
	events    []*AgentEvent
}

func NewMockParser(agentType string) *MockParser {
	return &MockParser{agentType: agentType}
}

func (p *MockParser) AgentType() string {
	return p.agentType
}

func (p *MockParser) Parse(stream string, content []byte) ([]*AgentEvent, error) {
	return p.events, nil
}

func (p *MockParser) Flush() ([]*AgentEvent, error) {
	return nil, nil
}

func (p *MockParser) Reset() {
	p.events = nil
}

func TestParserRegistry_NewParserRegistry(t *testing.T) {
	r := NewParserRegistry()
	assert.NotNil(t, r)
	assert.Empty(t, r.AgentTypes())
}

func TestParserRegistry_Register(t *testing.T) {
	r := NewParserRegistry()

	p1 := NewMockParser("claude")
	p2 := NewMockParser("codex")

	r.Register(p1)
	r.Register(p2)

	types := r.AgentTypes()
	assert.Len(t, types, 2)
	assert.Contains(t, types, "claude")
	assert.Contains(t, types, "codex")
}

func TestParserRegistry_Get(t *testing.T) {
	r := NewParserRegistry()

	p := NewMockParser("claude")
	r.Register(p)

	// Get existing parser
	got := r.Get("claude")
	require.NotNil(t, got)
	assert.Equal(t, "claude", got.AgentType())

	// Get non-existing parser
	got = r.Get("unknown")
	assert.Nil(t, got)
}

func TestParserRegistry_RegisterOverwrite(t *testing.T) {
	r := NewParserRegistry()

	p1 := NewMockParser("claude")
	p2 := NewMockParser("claude") // Same type, different instance

	r.Register(p1)
	r.Register(p2)

	// Should only have one
	types := r.AgentTypes()
	assert.Len(t, types, 1)

	// Should be the second one
	got := r.Get("claude")
	assert.Same(t, p2, got)
}
