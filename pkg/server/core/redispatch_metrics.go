package core

import "github.com/prometheus/client_golang/prometheus"

// Wake trigger labels. They are the metric dimension that makes trigger noise
// attributable: "the sweep is doing all the work" and "every runner that goes
// idle wakes a pass that finds nothing" look identical without them.
const (
	// WakeTriggerRunnerFreed is a runner handed back by a session - suspended,
	// detached, terminated, or simply finished its task and went idle.
	WakeTriggerRunnerFreed = "runner_freed"

	// WakeTriggerRunnerJoined is a runner that connected or reconnected and
	// reached idle. A pool that was empty when a task was created never gets
	// revisited without this.
	WakeTriggerRunnerJoined = "runner_joined"

	// WakeTriggerSweep is the timer backstop. Every other trigger is an edge,
	// and edges are missed: a restart loses in-memory state, a runner frees up
	// in a way nothing watches.
	WakeTriggerSweep = "sweep"
)

// RedispatchMetrics counts what the redispatch triggers do.
//
// The design rule the proposal rests on is that wakes are cheap and idempotent,
// so any number of triggers may fire around one event. That is only a
// defensible rule if the noise is visible: registration, the sweep and a freed
// runner can all fire for the same runner, and without these counters the
// difference between "coalesced correctly" and "storming" cannot be seen from
// outside the process.
//
// Every method is nil-safe so tests and embedders can leave it unset.
type RedispatchMetrics struct {
	wakes             *prometheus.CounterVec
	wakesCoalesced    *prometheus.CounterVec
	passes            *prometheus.CounterVec
	sessionsWoken     *prometheus.CounterVec
	runnersAllocated  prometheus.Counter
	allocationsFailed prometheus.Counter
	tasksParked       prometheus.Counter
	dispatchFailures  prometheus.Counter
}

// NewRedispatchMetrics builds the counters and registers them on reg.
//
// A nil registerer builds the counters anyway and registers nothing: the
// metrics subsystem is optional (observability.metrics.enabled), and the code
// that increments these must not have to care whether it is on.
func NewRedispatchMetrics(reg prometheus.Registerer, namespace string) *RedispatchMetrics {
	if namespace == "" {
		namespace = "marionette"
	}

	m := &RedispatchMetrics{
		wakes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "redispatch_wakes_total",
			Help:      "Redispatch wakes requested, by trigger",
		}, []string{"trigger"}),
		wakesCoalesced: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "redispatch_wakes_coalesced_total",
			Help:      "Redispatch wakes folded into a pass that was already running, by trigger",
		}, []string{"trigger"}),
		passes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "redispatch_passes_total",
			Help:      "Redispatch passes completed, by outcome (dispatched, empty, failed)",
		}, []string{"outcome"}),
		sessionsWoken: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "redispatch_sessions_woken_total",
			Help:      "Sessions whose backlog a redispatch pass moved, by trigger",
		}, []string{"trigger"}),
		runnersAllocated: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "redispatch_runners_allocated_total",
			Help:      "Runners a redispatch pass allocated to a session that had none",
		}),
		allocationsFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "redispatch_runner_allocations_failed_total",
			Help:      "Redispatch attempts that found no runner for a parked session",
		}),
		tasksParked: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "redispatch_tasks_parked_total",
			Help:      "Tasks that exhausted their redispatch budget and now wait for a human",
		}),
		dispatchFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "redispatch_dispatch_failures_total",
			Help:      "Dispatch attempts that failed to reach a runner and backed off",
		}),
	}

	if reg != nil {
		reg.MustRegister(
			m.wakes,
			m.wakesCoalesced,
			m.passes,
			m.sessionsWoken,
			m.runnersAllocated,
			m.allocationsFailed,
			m.tasksParked,
			m.dispatchFailures,
		)
	}
	return m
}

// Pass outcomes.
const (
	passOutcomeDispatched = "dispatched"
	passOutcomeEmpty      = "empty"
	passOutcomeFailed     = "failed"
)

func (m *RedispatchMetrics) wakeRequested(trigger string) {
	if m == nil {
		return
	}
	m.wakes.WithLabelValues(trigger).Inc()
}

func (m *RedispatchMetrics) wakeCoalesced(trigger string) {
	if m == nil {
		return
	}
	m.wakesCoalesced.WithLabelValues(trigger).Inc()
}

func (m *RedispatchMetrics) passCompleted(outcome string) {
	if m == nil {
		return
	}
	m.passes.WithLabelValues(outcome).Inc()
}

func (m *RedispatchMetrics) sessionWoken(trigger string) {
	if m == nil {
		return
	}
	m.sessionsWoken.WithLabelValues(trigger).Inc()
}

func (m *RedispatchMetrics) runnerAllocated() {
	if m == nil {
		return
	}
	m.runnersAllocated.Inc()
}

func (m *RedispatchMetrics) allocationFailed() {
	if m == nil {
		return
	}
	m.allocationsFailed.Inc()
}

func (m *RedispatchMetrics) taskParked() {
	if m == nil {
		return
	}
	m.tasksParked.Inc()
}

func (m *RedispatchMetrics) dispatchFailed() {
	if m == nil {
		return
	}
	m.dispatchFailures.Inc()
}
