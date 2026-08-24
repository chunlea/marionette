package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// Runner label filtering was a TODO in listRunners while callers were already
// passing labels: runner selection hands a profile's os and arch selectors to
// it, and with the filter missing a session that asked for linux/arm64 was
// served whatever idle runner came first.

func TestListRunnersFiltersByLabels(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().Format("150405.000000")

	linuxArm := &store.Runner{
		Name:         "label-linux-arm-" + suffix,
		Status:       "idle",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
		Labels:       json.RawMessage(`{"os":"linux","arch":"arm64","tier":"gold"}`),
	}
	linuxAmd := &store.Runner{
		Name:         "label-linux-amd-" + suffix,
		Status:       "idle",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
		Labels:       json.RawMessage(`{"os":"linux","arch":"amd64"}`),
	}
	unlabelled := &store.Runner{
		Name:         "label-none-" + suffix,
		Status:       "idle",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	for _, runner := range []*store.Runner{linuxArm, linuxAmd, unlabelled} {
		require.NoError(t, testStore.CreateRunner(ctx, runner))
		t.Cleanup(func() { _ = testStore.DeleteRunner(context.Background(), runner.ID) })
	}

	t.Run("one label selects the runners that carry it", func(t *testing.T) {
		result, err := testStore.ListRunners(ctx, store.ListRunnersOptions{
			BaseListOptions: store.BaseListOptions{Limit: 100},
			Labels:          map[string]string{"arch": "arm64"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{linuxArm.ID}, runnerIDsIn(result, linuxArm.ID, linuxAmd.ID, unlabelled.ID))
	})

	t.Run("labels are ANDed", func(t *testing.T) {
		result, err := testStore.ListRunners(ctx, store.ListRunnersOptions{
			BaseListOptions: store.BaseListOptions{Limit: 100},
			Labels:          map[string]string{"os": "linux", "arch": "amd64"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{linuxAmd.ID}, runnerIDsIn(result, linuxArm.ID, linuxAmd.ID, unlabelled.ID))
	})

	t.Run("matching is containment, not equality", func(t *testing.T) {
		result, err := testStore.ListRunners(ctx, store.ListRunnersOptions{
			BaseListOptions: store.BaseListOptions{Limit: 100},
			Labels:          map[string]string{"os": "linux"},
		})
		require.NoError(t, err)
		assert.ElementsMatch(t,
			[]string{linuxArm.ID, linuxAmd.ID},
			runnerIDsIn(result, linuxArm.ID, linuxAmd.ID, unlabelled.ID),
			"a runner with extra labels still matches a subset filter")
	})

	t.Run("a label nobody carries selects nothing", func(t *testing.T) {
		result, err := testStore.ListRunners(ctx, store.ListRunnersOptions{
			BaseListOptions: store.BaseListOptions{Limit: 100},
			Labels:          map[string]string{"os": "plan9"},
		})
		require.NoError(t, err)
		assert.Empty(t, runnerIDsIn(result, linuxArm.ID, linuxAmd.ID, unlabelled.ID))
	})

	t.Run("the count matches the filter", func(t *testing.T) {
		result, err := testStore.ListRunners(ctx, store.ListRunnersOptions{
			BaseListOptions: store.BaseListOptions{Limit: 100},
			Labels:          map[string]string{"tier": "gold"},
		})
		require.NoError(t, err)
		// TotalCount comes from a separate COUNT query; a filter applied to one
		// and not the other is how has_more starts lying.
		assert.Equal(t, int64(len(result.Items)), result.TotalCount)
	})
}

// runnerIDsIn narrows a result to the runners this test created, so a shared
// database with rows from other tests cannot make the assertions flaky.
func runnerIDsIn(result *store.ListResult[store.Runner], ids ...string) []string {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}

	var found []string
	for _, runner := range result.Items {
		if _, ok := wanted[runner.ID]; ok {
			found = append(found, runner.ID)
		}
	}
	return found
}

// The sequence range is what the fan-out relay reads a notified batch back
// with. Sequence is unique per run, so the range is always paired with a run.
func TestListLogsFiltersBySequenceRange(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().Format("150405.000000")

	workspace := &store.Workspace{
		Name:        "seq-ws-" + suffix,
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	require.NoError(t, testStore.CreateWorkspace(ctx, workspace))

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	require.NoError(t, testStore.CreateSession(ctx, session))

	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "sequence range",
		Status:         "running",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}
	require.NoError(t, testStore.CreateTask(ctx, task))

	runner := &store.Runner{
		Name:         "seq-runner-" + suffix,
		Status:       "busy",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	require.NoError(t, testStore.CreateRunner(ctx, runner))
	t.Cleanup(func() { _ = testStore.DeleteRunner(context.Background(), runner.ID) })

	run := &store.TaskRun{TaskID: task.ID, Attempt: 1, Status: "running", RunnerID: &runner.ID}
	require.NoError(t, testStore.CreateTaskRun(ctx, run))

	logs := make([]*store.Log, 0, 5)
	for seq := int64(1); seq <= 5; seq++ {
		logs = append(logs, &store.Log{
			SessionID: session.ID,
			TaskID:    task.ID,
			RunID:     run.ID,
			RunnerID:  runner.ID,
			Stream:    "stdout",
			Level:     "info",
			Content:   "line",
			Sequence:  seq,
		})
	}
	require.NoError(t, testStore.CreateLogs(ctx, logs))

	minSeq, maxSeq := int64(2), int64(4)
	result, err := testStore.ListLogs(ctx, store.ListLogsOptions{
		BaseListOptions: store.BaseListOptions{Limit: 100},
		RunID:           &run.ID,
		MinSequence:     &minSeq,
		MaxSequence:     &maxSeq,
	})
	require.NoError(t, err)

	var got []int64
	for _, log := range result.Items {
		got = append(got, log.Sequence)
	}
	assert.ElementsMatch(t, []int64{2, 3, 4}, got, "the range is inclusive at both ends")

	// An open upper bound is the shape a tail uses.
	result, err = testStore.ListLogs(ctx, store.ListLogsOptions{
		BaseListOptions: store.BaseListOptions{Limit: 100},
		RunID:           &run.ID,
		MinSequence:     &maxSeq,
	})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2, "min alone leaves the upper end open")
}
