// Package executor provides interfaces and implementations for running AI agents.
package executor

// AgentEventParser parses raw agent output into structured events.
// Each agent type implements this interface to convert its native format
// to the unified AgentEvent format.
//
// Plugin developers implementing a new agent executor must also provide
// a corresponding parser implementation.
type AgentEventParser interface {
	// AgentType returns the agent type this parser handles (e.g., "claude", "codex").
	AgentType() string

	// Parse parses a raw log entry and returns zero or more events.
	// The stream parameter indicates the source: "stdout", "stderr", or "json".
	// A single raw log entry may produce multiple events (e.g., a JSON message
	// containing both text and tool use).
	//
	// Returns nil/empty slice if the content produces no events.
	Parse(stream string, content []byte) ([]*AgentEvent, error)

	// Flush is called when a task completes to emit any pending events.
	// Some parsers may buffer partial content (e.g., incomplete JSON lines)
	// and need to flush them at the end.
	//
	// Returns nil/empty slice if no pending events.
	Flush() ([]*AgentEvent, error)

	// Reset clears any internal state for a new task.
	Reset()
}

// ParserRegistry manages agent event parsers.
type ParserRegistry struct {
	parsers map[string]AgentEventParser
}

// NewParserRegistry creates a new parser registry.
func NewParserRegistry() *ParserRegistry {
	return &ParserRegistry{
		parsers: make(map[string]AgentEventParser),
	}
}

// Register adds a parser to the registry.
func (r *ParserRegistry) Register(parser AgentEventParser) {
	r.parsers[parser.AgentType()] = parser
}

// Get returns the parser for the given agent type.
// Returns nil if no parser is registered.
func (r *ParserRegistry) Get(agentType string) AgentEventParser {
	return r.parsers[agentType]
}

// AgentTypes returns all registered agent types.
func (r *ParserRegistry) AgentTypes() []string {
	types := make([]string, 0, len(r.parsers))
	for t := range r.parsers {
		types = append(types, t)
	}
	return types
}
