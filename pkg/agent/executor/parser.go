package executor

import (
	"errors"
	"sync"
)

var (
	// ErrUnknownAgent is returned when no parser is registered for the agent type.
	ErrUnknownAgent = errors.New("unknown agent type")

	// ErrParseError is returned when parsing fails.
	ErrParseError = errors.New("parse error")
)

// AgentEventParser parses raw agent output into structured events.
// Each agent type (claude, codex, etc.) implements its own parser.
type AgentEventParser interface {
	// ParseLine parses a single line of output and returns events.
	// A single line may produce zero, one, or multiple events.
	// Returns nil if the line doesn't contain a parseable event.
	ParseLine(line []byte) ([]*AgentEvent, error)

	// Flush returns any buffered events that haven't been emitted yet.
	// Called when the agent process ends.
	Flush() ([]*AgentEvent, error)

	// Reset clears any internal state.
	// Called when starting a new task.
	Reset()
}

// ParserFactory creates a new parser instance for an agent type.
type ParserFactory func() AgentEventParser

// ParserRegistry manages parser factories for different agent types.
type ParserRegistry struct {
	mu      sync.RWMutex
	parsers map[string]ParserFactory
}

// NewParserRegistry creates a new parser registry.
func NewParserRegistry() *ParserRegistry {
	return &ParserRegistry{
		parsers: make(map[string]ParserFactory),
	}
}

// Register registers a parser factory for an agent type.
func (r *ParserRegistry) Register(agent string, factory ParserFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parsers[agent] = factory
}

// Get returns a new parser instance for the given agent type.
// Returns ErrUnknownAgent if no parser is registered.
func (r *ParserRegistry) Get(agent string) (AgentEventParser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, ok := r.parsers[agent]
	if !ok {
		return nil, ErrUnknownAgent
	}
	return factory(), nil
}

// Has returns true if a parser is registered for the agent type.
func (r *ParserRegistry) Has(agent string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.parsers[agent]
	return ok
}

// Agents returns a list of registered agent types.
func (r *ParserRegistry) Agents() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agents := make([]string, 0, len(r.parsers))
	for agent := range r.parsers {
		agents = append(agents, agent)
	}
	return agents
}

// DefaultRegistry is the global parser registry.
var DefaultRegistry = NewParserRegistry()

// RegisterParser registers a parser factory with the default registry.
func RegisterParser(agent string, factory ParserFactory) {
	DefaultRegistry.Register(agent, factory)
}

// GetParser returns a parser from the default registry.
func GetParser(agent string) (AgentEventParser, error) {
	return DefaultRegistry.Get(agent)
}
