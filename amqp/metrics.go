package amqp

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ConnectionMetrics holds Prometheus metrics for the AMQP connection layer.
// A nil pointer is safe to use — all methods are no-ops when the receiver is nil.
type ConnectionMetrics struct {
	connectionUp      *prometheus.GaugeVec
	reconnectAttempts *prometheus.CounterVec
	reconnectDuration *prometheus.HistogramVec
}

func NewConnectionMetrics(namespace string) *ConnectionMetrics {
	return &ConnectionMetrics{
		connectionUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "amqp",
			Name:      "connection_up",
			Help:      "1 if the AMQP connection is established, 0 while disconnected.",
		}, []string{"role"}),

		reconnectAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "amqp",
			Name:      "reconnect_attempts_total",
			Help:      "Total number of failed AMQP dial attempts.",
		}, []string{"role"}),

		reconnectDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "amqp",
			Name:      "reconnect_duration_seconds",
			Help:      "Time from connection loss to successful reconnect, in seconds.",
			Buckets:   []float64{1, 2, 3, 5, 10, 30, 60},
		}, []string{"role"}),
	}
}

func (m *ConnectionMetrics) Describe(ch chan<- *prometheus.Desc) {
	if m == nil {
		return
	}
	m.connectionUp.Describe(ch)
	m.reconnectAttempts.Describe(ch)
	m.reconnectDuration.Describe(ch)
}

func (m *ConnectionMetrics) Collect(ch chan<- prometheus.Metric) {
	if m == nil {
		return
	}
	m.connectionUp.Collect(ch)
	m.reconnectAttempts.Collect(ch)
	m.reconnectDuration.Collect(ch)
}

func (m *ConnectionMetrics) setConnectionUp(role string, v float64) {
	if m == nil {
		return
	}
	m.connectionUp.WithLabelValues(role).Set(v)
}

func (m *ConnectionMetrics) incReconnectAttempts(role string) {
	if m == nil {
		return
	}
	m.reconnectAttempts.WithLabelValues(role).Inc()
}

func (m *ConnectionMetrics) observeReconnectDuration(role string, d time.Duration) {
	if m == nil {
		return
	}
	m.reconnectDuration.WithLabelValues(role).Observe(d.Seconds())
}
