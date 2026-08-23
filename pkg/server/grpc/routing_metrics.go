package grpc

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Routing path labels.
const (
	// routePathLocal is a command written to a stream this process holds, and
	// the only path a single-process deployment ever takes.
	routePathLocal = "local"
	// routePathRemote is a command handed to the replica that holds the
	// stream. If this is non-zero in a deployment the operator believes is
	// single-replica, something is wrong with the deployment, not the code.
	routePathRemote = "remote"
)

// Send outcome labels. They mirror the errors SendCommand returns, because the
// question an operator asks of this metric is the same question the caller
// asks of the error.
const (
	outcomeDelivered    = "delivered"
	outcomeNotConnected = "not_connected"
	outcomeQueueFull    = "queue_full"
	outcomeDisconnected = "disconnected"
	outcomeUnreachable  = "unreachable"
	outcomeError        = "error"
)

// RoutingMetrics counts what command routing does.
//
// The design rests on two claims that are invisible from outside the process
// without these: that a single-process deployment never leaves the local path,
// and that a cross-replica send fails loudly rather than silently. Both are
// assertions about production, not about tests, so they need a counter.
//
// Every method is nil-safe: metrics are optional
// (observability.metrics.enabled) and the send path must not care.
type RoutingMetrics struct {
	sends    *prometheus.CounterVec
	hopTime  prometheus.Histogram
	conflict prometheus.Counter
	replicas prometheus.Gauge
}

// NewRoutingMetrics builds the collectors and registers them on reg.
//
// A nil registerer builds them anyway and registers nothing, so the callers
// that increment them do not have to know whether metrics are on.
func NewRoutingMetrics(reg prometheus.Registerer, namespace string) *RoutingMetrics {
	if namespace == "" {
		namespace = "marionette"
	}

	m := &RoutingMetrics{
		sends: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "runner_command_sends_total",
			Help:      "Commands sent to runners, by path (local, remote) and outcome",
		}, []string{"path", "outcome"}),
		hopTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "runner_command_hop_seconds",
			Help:      "Time spent handing a command to the replica holding the runner",
			Buckets:   prometheus.DefBuckets,
		}),
		conflict: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "runner_connection_registry_conflicts_total",
			Help: "Times the local connection map and the routing registry disagreed " +
				"about who holds a runner",
		}),
		replicas: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "server_replicas_live",
			Help:      "Server replicas currently heartbeating",
		}),
	}

	if reg != nil {
		reg.MustRegister(m.sends, m.hopTime, m.conflict, m.replicas)
	}
	return m
}

// send records one send attempt and its outcome.
func (m *RoutingMetrics) send(path string, err error) {
	if m == nil {
		return
	}
	m.sends.WithLabelValues(path, sendOutcome(err)).Inc()
}

// hop records how long one cross-replica delivery took.
func (m *RoutingMetrics) hop(d time.Duration) {
	if m == nil {
		return
	}
	m.hopTime.Observe(d.Seconds())
}

// conflictSeen records that the local map and the registry disagreed. Expected
// to be ~0; if it is not, an assumption in the design is wrong.
func (m *RoutingMetrics) conflictSeen() {
	if m == nil {
		return
	}
	m.conflict.Inc()
}

// SetLiveReplicas publishes how many replicas are heartbeating.
//
// Exported because the thing that knows the number is core.ReplicaRegistry,
// and core cannot import this package - grpc imports core, not the other way
// round. cmd/server hands the registry this method as its observer.
func (m *RoutingMetrics) SetLiveReplicas(n int) {
	if m == nil {
		return
	}
	m.replicas.Set(float64(n))
}

func sendOutcome(err error) string {
	switch {
	case err == nil:
		return outcomeDelivered
	case errors.Is(err, ErrRunnerNotFound):
		return outcomeNotConnected
	case errors.Is(err, ErrCommandQueueFull):
		return outcomeQueueFull
	case errors.Is(err, ErrRunnerDisconnected):
		return outcomeDisconnected
	case errors.Is(err, ErrReplicaUnreachable):
		return outcomeUnreachable
	default:
		return outcomeError
	}
}
