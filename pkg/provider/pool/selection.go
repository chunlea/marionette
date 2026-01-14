package pool

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"sync"

	"github.com/chunlea/marionette/pkg/store"
)

// ListRunnersStore is the subset of store.Store needed by the selector.
type ListRunnersStore interface {
	ListRunners(ctx context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error)
}

// Selector handles runner selection from the pool.
type Selector struct {
	store    ListRunnersStore
	poolName string
	strategy string

	// For round-robin selection
	mu          sync.Mutex
	lastIndex   int
	runnerOrder []string
}

// NewSelector creates a new runner selector.
func NewSelector(st ListRunnersStore, poolName, strategy string) *Selector {
	if strategy == "" {
		strategy = "lru"
	}
	return &Selector{
		store:    st,
		poolName: poolName,
		strategy: strategy,
	}
}

// SelectionCriteria defines requirements for runner selection.
type SelectionCriteria struct {
	// RequiredLabels are labels the runner must have.
	RequiredLabels map[string]string

	// RequiredCapabilities are capabilities the runner must have.
	RequiredCapabilities []string

	// PreferRunnerID prefers a specific runner if available (for resume).
	PreferRunnerID string

	// ExcludeRunnerIDs excludes specific runners from selection.
	ExcludeRunnerIDs []string

	// ExcludeTainted excludes tainted runners from selection.
	ExcludeTainted bool
}

// SelectRunner selects an idle runner from the pool based on the criteria.
// Returns nil if no suitable runner is found.
func (s *Selector) SelectRunner(ctx context.Context, criteria SelectionCriteria) (*store.Runner, error) {
	// Get all idle runners in the pool
	poolName := s.poolName
	idleStatus := []string{"idle"}
	opts := store.ListRunnersOptions{
		PoolName: &poolName,
		Status:   idleStatus,
		Labels:   criteria.RequiredLabels,
	}

	if criteria.ExcludeTainted {
		tainted := false
		opts.Tainted = &tainted
	}

	result, err := s.store.ListRunners(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("listing runners: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, nil
	}

	// Filter by additional criteria
	candidates := s.filterCandidates(result.Items, criteria)
	if len(candidates) == 0 {
		return nil, nil
	}

	// Check for preferred runner first
	if criteria.PreferRunnerID != "" {
		for _, r := range candidates {
			if r.ID == criteria.PreferRunnerID {
				return r, nil
			}
		}
	}

	// Select based on strategy
	switch s.strategy {
	case "lru":
		return s.selectLRU(candidates), nil
	case "random":
		return s.selectRandom(candidates), nil
	case "round_robin":
		return s.selectRoundRobin(candidates), nil
	default:
		return s.selectLRU(candidates), nil
	}
}

// filterCandidates filters runners by additional criteria.
func (s *Selector) filterCandidates(runners []*store.Runner, criteria SelectionCriteria) []*store.Runner {
	excludeSet := make(map[string]bool)
	for _, id := range criteria.ExcludeRunnerIDs {
		excludeSet[id] = true
	}

	var candidates []*store.Runner
	for _, r := range runners {
		// Skip excluded runners
		if excludeSet[r.ID] {
			continue
		}

		// Check capabilities
		if !s.hasCapabilities(r, criteria.RequiredCapabilities) {
			continue
		}

		candidates = append(candidates, r)
	}

	return candidates
}

// hasCapabilities checks if a runner has all required capabilities.
func (s *Selector) hasCapabilities(runner *store.Runner, required []string) bool {
	if len(required) == 0 {
		return true
	}

	capSet := make(map[string]bool)
	for _, cap := range runner.Capabilities {
		capSet[cap] = true
	}

	for _, req := range required {
		if !capSet[req] {
			return false
		}
	}

	return true
}

// selectLRU selects the least recently used runner (oldest last_seen_at).
func (s *Selector) selectLRU(candidates []*store.Runner) *store.Runner {
	if len(candidates) == 0 {
		return nil
	}

	// Sort by last_seen_at ascending (oldest first)
	sort.Slice(candidates, func(i, j int) bool {
		ti := candidates[i].LastSeenAt
		tj := candidates[j].LastSeenAt
		if ti == nil && tj == nil {
			return candidates[i].ID < candidates[j].ID
		}
		if ti == nil {
			return true // nil comes first
		}
		if tj == nil {
			return false
		}
		return ti.Before(*tj)
	})

	return candidates[0]
}

// selectRandom selects a random runner from candidates.
func (s *Selector) selectRandom(candidates []*store.Runner) *store.Runner {
	if len(candidates) == 0 {
		return nil
	}
	return candidates[rand.Intn(len(candidates))]
}

// selectRoundRobin selects runners in round-robin order.
func (s *Selector) selectRoundRobin(candidates []*store.Runner) *store.Runner {
	if len(candidates) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build current runner ID list
	currentIDs := make([]string, len(candidates))
	for i, r := range candidates {
		currentIDs[i] = r.ID
	}
	sort.Strings(currentIDs)

	// Rebuild order if candidates changed
	if !stringSlicesEqual(currentIDs, s.runnerOrder) {
		s.runnerOrder = currentIDs
		s.lastIndex = 0
	}

	// Find next valid runner
	for range candidates {
		s.lastIndex = (s.lastIndex + 1) % len(s.runnerOrder)
		targetID := s.runnerOrder[s.lastIndex]

		for _, r := range candidates {
			if r.ID == targetID {
				return r
			}
		}
	}

	// Fallback to first candidate
	return candidates[0]
}

// MatchesSelector checks if a runner matches a profile selector.
func MatchesSelector(runner *store.Runner, selector json.RawMessage) (bool, error) {
	if len(selector) == 0 || string(selector) == "null" || string(selector) == "{}" {
		return true, nil // Empty selector matches all
	}

	var sel map[string]string
	if err := json.Unmarshal(selector, &sel); err != nil {
		return false, fmt.Errorf("parsing selector: %w", err)
	}

	// Parse runner labels
	var runnerLabels map[string]string
	if len(runner.Labels) > 0 {
		if err := json.Unmarshal(runner.Labels, &runnerLabels); err != nil {
			return false, fmt.Errorf("parsing runner labels: %w", err)
		}
	}

	// All selector labels must match
	for k, v := range sel {
		if runnerLabels[k] != v {
			return false, nil
		}
	}

	return true, nil
}

// stringSlicesEqual checks if two string slices are equal.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
