package pool

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// mockListRunnersStore is a simple mock for testing selection
type mockListRunnersStore struct {
	runners []*store.Runner
	err     error
}

func (m *mockListRunnersStore) ListRunners(_ context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	if m.err != nil {
		return nil, m.err
	}

	var filtered []*store.Runner
	for _, r := range m.runners {
		// Filter by pool name
		if opts.PoolName != nil && (r.PoolName == nil || *r.PoolName != *opts.PoolName) {
			continue
		}

		// Filter by status
		if len(opts.Status) > 0 {
			found := false
			for _, s := range opts.Status {
				if r.Status == s {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Filter by tainted
		if opts.Tainted != nil && r.Tainted != *opts.Tainted {
			continue
		}

		// Filter by labels
		if opts.Labels != nil {
			var runnerLabels map[string]string
			if len(r.Labels) > 0 {
				_ = json.Unmarshal(r.Labels, &runnerLabels)
			}
			match := true
			for k, v := range opts.Labels {
				if runnerLabels[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}

		filtered = append(filtered, r)
	}

	return &store.ListResult[store.Runner]{Items: filtered, TotalCount: int64(len(filtered))}, nil
}

func TestNewSelector(t *testing.T) {
	ms := &mockListRunnersStore{}

	s := NewSelector(ms, "test-pool", "lru")
	assert.Equal(t, "test-pool", s.poolName)
	assert.Equal(t, "lru", s.strategy)

	s2 := NewSelector(ms, "test", "")
	assert.Equal(t, "lru", s2.strategy, "empty strategy should default to lru")
}

func TestSelectRunner_NoRunners(t *testing.T) {
	ms := &mockListRunnersStore{runners: []*store.Runner{}}
	s := NewSelector(ms, "test-pool", "lru")

	runner, err := s.SelectRunner(context.Background(), SelectionCriteria{})
	require.NoError(t, err)
	assert.Nil(t, runner)
}

func TestSelectRunner_LRU(t *testing.T) {
	poolName := "test-pool"
	now := time.Now()
	old := now.Add(-10 * time.Minute)
	older := now.Add(-20 * time.Minute)

	ms := &mockListRunnersStore{
		runners: []*store.Runner{
			{ID: "run_1", Status: "idle", PoolName: &poolName, LastSeenAt: &now},
			{ID: "run_2", Status: "idle", PoolName: &poolName, LastSeenAt: &old},
			{ID: "run_3", Status: "idle", PoolName: &poolName, LastSeenAt: &older},
		},
	}
	s := NewSelector(ms, "test-pool", "lru")

	runner, err := s.SelectRunner(context.Background(), SelectionCriteria{})
	require.NoError(t, err)
	require.NotNil(t, runner)
	assert.Equal(t, "run_3", runner.ID, "should select oldest runner")
}

func TestSelectRunner_LRU_NilLastSeenAt(t *testing.T) {
	poolName := "test-pool"
	now := time.Now()

	ms := &mockListRunnersStore{
		runners: []*store.Runner{
			{ID: "run_1", Status: "idle", PoolName: &poolName, LastSeenAt: &now},
			{ID: "run_2", Status: "idle", PoolName: &poolName, LastSeenAt: nil}, // nil comes first
		},
	}
	s := NewSelector(ms, "test-pool", "lru")

	runner, err := s.SelectRunner(context.Background(), SelectionCriteria{})
	require.NoError(t, err)
	require.NotNil(t, runner)
	assert.Equal(t, "run_2", runner.ID, "nil LastSeenAt should come first")
}

func TestSelectRunner_Random(t *testing.T) {
	poolName := "test-pool"

	ms := &mockListRunnersStore{
		runners: []*store.Runner{
			{ID: "run_1", Status: "idle", PoolName: &poolName},
			{ID: "run_2", Status: "idle", PoolName: &poolName},
			{ID: "run_3", Status: "idle", PoolName: &poolName},
		},
	}
	s := NewSelector(ms, "test-pool", "random")

	// Run multiple times to verify randomness
	selectedIDs := make(map[string]int)
	for i := 0; i < 30; i++ {
		runner, err := s.SelectRunner(context.Background(), SelectionCriteria{})
		require.NoError(t, err)
		require.NotNil(t, runner)
		selectedIDs[runner.ID]++
	}

	// Should have selected at least 1 runner (random might pick same one)
	assert.GreaterOrEqual(t, len(selectedIDs), 1, "random selection should work")
}

func TestSelectRunner_RoundRobin(t *testing.T) {
	poolName := "test-pool"

	ms := &mockListRunnersStore{
		runners: []*store.Runner{
			{ID: "run_1", Status: "idle", PoolName: &poolName},
			{ID: "run_2", Status: "idle", PoolName: &poolName},
			{ID: "run_3", Status: "idle", PoolName: &poolName},
		},
	}
	s := NewSelector(ms, "test-pool", "round_robin")

	// Round robin should cycle through runners
	selectedIDs := make([]string, 6)
	for i := 0; i < 6; i++ {
		runner, err := s.SelectRunner(context.Background(), SelectionCriteria{})
		require.NoError(t, err)
		require.NotNil(t, runner)
		selectedIDs[i] = runner.ID
	}

	// Verify cycling (after first full cycle, pattern should repeat)
	assert.Equal(t, selectedIDs[0], selectedIDs[3], "round robin should cycle")
	assert.Equal(t, selectedIDs[1], selectedIDs[4], "round robin should cycle")
	assert.Equal(t, selectedIDs[2], selectedIDs[5], "round robin should cycle")
}

func TestSelectRunner_PreferredRunner(t *testing.T) {
	poolName := "test-pool"
	now := time.Now()
	old := now.Add(-10 * time.Minute)

	ms := &mockListRunnersStore{
		runners: []*store.Runner{
			{ID: "run_1", Status: "idle", PoolName: &poolName, LastSeenAt: &old},
			{ID: "run_2", Status: "idle", PoolName: &poolName, LastSeenAt: &now},
		},
	}
	s := NewSelector(ms, "test-pool", "lru")

	// Should select preferred runner even though LRU would pick run_1
	runner, err := s.SelectRunner(context.Background(), SelectionCriteria{
		PreferRunnerID: "run_2",
	})
	require.NoError(t, err)
	require.NotNil(t, runner)
	assert.Equal(t, "run_2", runner.ID)
}

func TestSelectRunner_ExcludeRunnerIDs(t *testing.T) {
	poolName := "test-pool"

	ms := &mockListRunnersStore{
		runners: []*store.Runner{
			{ID: "run_1", Status: "idle", PoolName: &poolName},
			{ID: "run_2", Status: "idle", PoolName: &poolName},
			{ID: "run_3", Status: "idle", PoolName: &poolName},
		},
	}
	s := NewSelector(ms, "test-pool", "lru")

	runner, err := s.SelectRunner(context.Background(), SelectionCriteria{
		ExcludeRunnerIDs: []string{"run_1", "run_2"},
	})
	require.NoError(t, err)
	require.NotNil(t, runner)
	assert.Equal(t, "run_3", runner.ID)
}

func TestSelectRunner_ExcludeTainted(t *testing.T) {
	poolName := "test-pool"

	ms := &mockListRunnersStore{
		runners: []*store.Runner{
			{ID: "run_1", Status: "idle", PoolName: &poolName, Tainted: true},
			{ID: "run_2", Status: "idle", PoolName: &poolName, Tainted: false},
		},
	}
	s := NewSelector(ms, "test-pool", "lru")

	runner, err := s.SelectRunner(context.Background(), SelectionCriteria{
		ExcludeTainted: true,
	})
	require.NoError(t, err)
	require.NotNil(t, runner)
	assert.Equal(t, "run_2", runner.ID)
}

func TestSelectRunner_RequiredCapabilities(t *testing.T) {
	poolName := "test-pool"

	ms := &mockListRunnersStore{
		runners: []*store.Runner{
			{ID: "run_1", Status: "idle", PoolName: &poolName, Capabilities: []string{"docker"}},
			{ID: "run_2", Status: "idle", PoolName: &poolName, Capabilities: []string{"docker", "cuda"}},
		},
	}
	s := NewSelector(ms, "test-pool", "lru")

	runner, err := s.SelectRunner(context.Background(), SelectionCriteria{
		RequiredCapabilities: []string{"cuda"},
	})
	require.NoError(t, err)
	require.NotNil(t, runner)
	assert.Equal(t, "run_2", runner.ID)
}

func TestSelectRunner_RequiredLabels(t *testing.T) {
	poolName := "test-pool"
	labels1, _ := json.Marshal(map[string]string{"gpu": "nvidia"})
	labels2, _ := json.Marshal(map[string]string{"gpu": "amd"})

	ms := &mockListRunnersStore{
		runners: []*store.Runner{
			{ID: "run_1", Status: "idle", PoolName: &poolName, Labels: labels1},
			{ID: "run_2", Status: "idle", PoolName: &poolName, Labels: labels2},
		},
	}
	s := NewSelector(ms, "test-pool", "lru")

	runner, err := s.SelectRunner(context.Background(), SelectionCriteria{
		RequiredLabels: map[string]string{"gpu": "nvidia"},
	})
	require.NoError(t, err)
	require.NotNil(t, runner)
	assert.Equal(t, "run_1", runner.ID)
}

func TestHasCapabilities(t *testing.T) {
	s := &Selector{}

	tests := []struct {
		name     string
		runner   *store.Runner
		required []string
		want     bool
	}{
		{
			name:     "no requirements",
			runner:   &store.Runner{Capabilities: []string{"docker"}},
			required: nil,
			want:     true,
		},
		{
			name:     "empty requirements",
			runner:   &store.Runner{Capabilities: []string{"docker"}},
			required: []string{},
			want:     true,
		},
		{
			name:     "has all required",
			runner:   &store.Runner{Capabilities: []string{"docker", "cuda", "gpu"}},
			required: []string{"docker", "cuda"},
			want:     true,
		},
		{
			name:     "missing one required",
			runner:   &store.Runner{Capabilities: []string{"docker"}},
			required: []string{"docker", "cuda"},
			want:     false,
		},
		{
			name:     "runner has no capabilities",
			runner:   &store.Runner{Capabilities: nil},
			required: []string{"docker"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.hasCapabilities(tt.runner, tt.required)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesSelector(t *testing.T) {
	tests := []struct {
		name     string
		runner   *store.Runner
		selector json.RawMessage
		want     bool
		wantErr  bool
	}{
		{
			name:     "empty selector matches all",
			runner:   &store.Runner{Labels: json.RawMessage(`{"region": "us-west"}`)},
			selector: json.RawMessage(`{}`),
			want:     true,
		},
		{
			name:     "null selector matches all",
			runner:   &store.Runner{Labels: json.RawMessage(`{"region": "us-west"}`)},
			selector: json.RawMessage(`null`),
			want:     true,
		},
		{
			name:     "nil selector matches all",
			runner:   &store.Runner{Labels: json.RawMessage(`{}`)},
			selector: nil,
			want:     true,
		},
		{
			name:     "matching selector",
			runner:   &store.Runner{Labels: json.RawMessage(`{"region": "us-west", "env": "prod"}`)},
			selector: json.RawMessage(`{"region": "us-west"}`),
			want:     true,
		},
		{
			name:     "non-matching selector",
			runner:   &store.Runner{Labels: json.RawMessage(`{"region": "us-east"}`)},
			selector: json.RawMessage(`{"region": "us-west"}`),
			want:     false,
		},
		{
			name:     "missing label",
			runner:   &store.Runner{Labels: json.RawMessage(`{}`)},
			selector: json.RawMessage(`{"region": "us-west"}`),
			want:     false,
		},
		{
			name:     "runner with no labels",
			runner:   &store.Runner{Labels: nil},
			selector: json.RawMessage(`{"region": "us-west"}`),
			want:     false,
		},
		{
			name:     "invalid selector json",
			runner:   &store.Runner{},
			selector: json.RawMessage(`{invalid`),
			wantErr:  true,
		},
		{
			name:     "invalid runner labels json",
			runner:   &store.Runner{Labels: json.RawMessage(`{invalid`)},
			selector: json.RawMessage(`{"region": "us-west"}`),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MatchesSelector(tt.runner, tt.selector)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStringSlicesEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{
			name: "equal slices",
			a:    []string{"a", "b", "c"},
			b:    []string{"a", "b", "c"},
			want: true,
		},
		{
			name: "different lengths",
			a:    []string{"a", "b"},
			b:    []string{"a", "b", "c"},
			want: false,
		},
		{
			name: "different content",
			a:    []string{"a", "b", "c"},
			b:    []string{"a", "b", "d"},
			want: false,
		},
		{
			name: "both empty",
			a:    []string{},
			b:    []string{},
			want: true,
		},
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringSlicesEqual(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}
