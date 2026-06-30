package amqp

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ConnectionMetrics holds Prometheus metrics for the AMQP connection layer.
type ConnectionMetrics struct {
	connectionUp    *prometheus.GaugeVec
	connectionLost  *prometheus.CounterVec
	dialFailures    *prometheus.CounterVec
	connectDuration *prometheus.SummaryVec
}

func NewConnectionMetrics(namespace string) *ConnectionMetrics {
	return &ConnectionMetrics{
		connectionUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "amqp",
			Name:      "connection_up",
			Help:      "1 if the AMQP connection is established, 0 while disconnected.",
		}, []string{"role"}),

		connectionLost: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "amqp",
			Name:      "connection_lost_total",
			Help:      "Total number of times an established AMQP connection was lost.",
		}, []string{"role"}),

		dialFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "amqp",
			Name:      "dial_failures_total",
			Help:      "Total number of failed AMQP dial attempts.",
		}, []string{"role"}),

		connectDuration: prometheus.NewSummaryVec(prometheus.SummaryOpts{
			Namespace: namespace,
			Subsystem: "amqp",
			Name:      "connect_duration_seconds",
			Help:      "Time from disconnect (or startup) to successful connect, in seconds.",
		}, []string{"role"}),
	}
}

func (m *ConnectionMetrics) Describe(ch chan<- *prometheus.Desc) {
	if m == nil {
		return
	}
	m.connectionUp.Describe(ch)
	m.connectionLost.Describe(ch)
	m.dialFailures.Describe(ch)
	m.connectDuration.Describe(ch)
}

func (m *ConnectionMetrics) Collect(ch chan<- prometheus.Metric) {
	if m == nil {
		return
	}
	m.connectionUp.Collect(ch)
	m.connectionLost.Collect(ch)
	m.dialFailures.Collect(ch)
	m.connectDuration.Collect(ch)
}

func (m *ConnectionMetrics) setConnectionUp(role string, v float64) {
	if m == nil {
		return
	}
	m.connectionUp.WithLabelValues(role).Set(v)
}

func (m *ConnectionMetrics) incConnectionLost(role string) {
	if m == nil {
		return
	}
	m.connectionLost.WithLabelValues(role).Inc()
}

func (m *ConnectionMetrics) incDialFailures(role string) {
	if m == nil {
		return
	}
	m.dialFailures.WithLabelValues(role).Inc()
}

func (m *ConnectionMetrics) observeConnectDuration(role string, d time.Duration) {
	if m == nil {
		return
	}
	m.connectDuration.WithLabelValues(role).Observe(d.Seconds())
}
