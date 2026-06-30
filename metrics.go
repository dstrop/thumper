package thumper

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	gopsprocess "github.com/shirou/gopsutil/process"
)

const namespace = "thumper"

type collector struct {
	plugin *Plugin

	consumerConcurrency *prometheus.GaugeVec

	messagesTotal   *prometheus.CounterVec
	processDuration *prometheus.SummaryVec

	phpWorkersTotal *prometheus.Desc
	phpWorkerMemory *prometheus.Desc
	phpWorkerCPU    *prometheus.Desc
	phpWorkerUptime *prometheus.Desc

	indexer *workerIndexer
}

func newCollector(plugin *Plugin) *collector {
	return &collector{
		plugin: plugin,

		consumerConcurrency: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "consumer_concurrency",
			Help:      "Configured concurrency limit per consumer.",
		}, []string{"consumer", "queue"}),

		messagesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "messages_total",
			Help:      "Total number of messages processed.",
		}, []string{"consumer", "queue", "result"}),

		processDuration: prometheus.NewSummaryVec(prometheus.SummaryOpts{
			Namespace: namespace,
			Name:      "processing_duration_seconds",
			Help:      "Message processing duration in seconds (PHP execution time).",
		}, []string{"consumer", "queue", "result"}),

		phpWorkersTotal: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "php_workers_total"),
			"Total number of PHP workers in the pool.",
			nil, nil,
		),
		phpWorkerMemory: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "php_worker_memory_bytes"),
			"RSS memory usage of a PHP worker.",
			[]string{"worker"}, nil,
		),
		phpWorkerCPU: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "php_worker_cpu_seconds_total"),
			"Total CPU time consumed by a PHP worker in seconds.",
			[]string{"worker"}, nil,
		),
		phpWorkerUptime: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "php_worker_uptime_seconds"),
			"Time in seconds since the PHP worker process was created.",
			[]string{"worker"}, nil,
		),

		indexer: &workerIndexer{
			pidToIdx: make(map[int64]int),
		},
	}
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	c.consumerConcurrency.Describe(ch)
	c.messagesTotal.Describe(ch)
	c.processDuration.Describe(ch)
	ch <- c.phpWorkersTotal
	ch <- c.phpWorkerMemory
	ch <- c.phpWorkerCPU
	ch <- c.phpWorkerUptime
}

func (c *collector) ObserveMessage(consumer, queue, result string, elapsed time.Duration) {
	c.messagesTotal.WithLabelValues(consumer, queue, result).Inc()
	c.processDuration.WithLabelValues(consumer, queue, result).Observe(elapsed.Seconds())
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	c.consumerConcurrency.Collect(ch)
	c.messagesTotal.Collect(ch)
	c.processDuration.Collect(ch)
	c.collectWorkers(ch)
}

func (c *collector) collectWorkers(ch chan<- prometheus.Metric) {
	c.plugin.mu.RLock()
	pool := c.plugin.pool
	c.plugin.mu.RUnlock()

	if pool == nil {
		return
	}

	workers := pool.Workers()
	pids := make([]int64, len(workers))
	for i, w := range workers {
		pids[i] = w.Pid()
	}
	indices := c.indexer.assign(pids)

	ch <- prometheus.MustNewConstMetric(c.phpWorkersTotal, prometheus.GaugeValue, float64(len(workers)))

	for _, w := range workers {
		label := strconv.Itoa(indices[w.Pid()])

		proc, err := gopsprocess.NewProcess(int32(w.Pid())) //nolint:gosec
		if err != nil {
			continue
		}

		if mem, err := proc.MemoryInfo(); err == nil {
			ch <- prometheus.MustNewConstMetric(c.phpWorkerMemory, prometheus.GaugeValue, float64(mem.RSS), label)
		}

		if times, err := proc.Times(); err == nil {
			ch <- prometheus.MustNewConstMetric(c.phpWorkerCPU, prometheus.CounterValue, times.User+times.System, label)
		}

		ch <- prometheus.MustNewConstMetric(c.phpWorkerUptime, prometheus.GaugeValue, time.Since(w.Created()).Seconds(), label)
	}
}

// workerIndexer assigns stable integer indices to worker PIDs, reusing slots
// when workers exit so that the index space doesn't grow unboundedly.
type workerIndexer struct {
	mu       sync.Mutex
	pidToIdx map[int64]int
	free     []int
	next     int
}

func (wi *workerIndexer) assign(pids []int64) map[int64]int {
	wi.mu.Lock()
	defer wi.mu.Unlock()

	seen := make(map[int64]struct{}, len(pids))
	for _, pid := range pids {
		seen[pid] = struct{}{}
	}

	for pid, idx := range wi.pidToIdx {
		if _, ok := seen[pid]; !ok {
			delete(wi.pidToIdx, pid)
			wi.free = append(wi.free, idx)
		}
	}

	result := make(map[int64]int, len(pids))
	for _, pid := range pids {
		if idx, ok := wi.pidToIdx[pid]; ok {
			result[pid] = idx
			continue
		}
		var idx int
		if len(wi.free) > 0 {
			idx = wi.free[0]
			wi.free = wi.free[1:]
		} else {
			idx = wi.next
			wi.next++
		}
		wi.pidToIdx[pid] = idx
		result[pid] = idx
	}

	return result
}
