package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dstrop/thumper/common"
	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/roadrunner-server/pool/payload"
	"go.uber.org/zap"
)

// Observer is an interface for observing message processing metrics.
type Observer interface {
	ObserveMessage(consumer, queue, result string, elapsed time.Duration)
}

const removeWorkerTimeout = 30 * time.Second

// Result bytes returned by a pool worker in the first byte of the payload body.
const (
	resultAck    byte = '0'
	resultNack   byte = '1'
	resultReject byte = '2'
)

// Worker manages a roadrunner pool and a set of reader goroutines that read jobs from a channel and execute them on the pool.
type Worker struct {
	pool       common.Pool
	numWorkers int
	log        *zap.Logger
	metrics    Observer

	jobs    <-chan common.Job
	wg      sync.WaitGroup
	quit    chan struct{}
	running bool
	mu      sync.Mutex
}

func New(pool common.Pool, numWorkers int, jobs <-chan common.Job, log *zap.Logger, metrics Observer) *Worker {
	return &Worker{
		pool:       pool,
		numWorkers: numWorkers,
		log:        log,
		metrics:    metrics,
		jobs:       jobs,
	}
}

// Start launches the reader goroutines.
func (w *Worker) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return
	}
	w.quit = make(chan struct{})
	w.running = true
	for i := 0; i < w.numWorkers; i++ {
		w.wg.Add(1)
		go w.readLoop()
	}
}

// Stop signals the readers to finish their current message and exit, then waits for them.
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	close(w.quit)
	w.running = false
	w.mu.Unlock()

	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Resize changes the number of PHP worker processes and reader goroutines.
func (w *Worker) Resize(n int) error {
	if n < 1 {
		return fmt.Errorf("worker count must be at least 1")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return fmt.Errorf("worker pool not running")
	}

	for w.numWorkers < n {
		if err := w.pool.AddWorker(); err != nil {
			return fmt.Errorf("add worker: %w", err)
		}
		w.wg.Add(1)
		go w.readLoop()
		w.numWorkers++
	}
	for w.numWorkers > n {
		ctx, cancel := context.WithTimeout(context.Background(), removeWorkerTimeout)
		err := w.pool.RemoveWorker(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("remove worker: %w", err)
		}
		w.quit <- struct{}{}
		w.numWorkers--
	}
	return nil
}

// readLoop is the main loop for a reader goroutine. It reads jobs from the channel and processes them.
func (w *Worker) readLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.quit:
			return
		case j := <-w.jobs:
			action := w.process(j.Queue, j.Consumer, j.Delivery)
			j.Settle(action)
		}
	}
}

// process builds a payload, runs it on the pool, and parses the result.
func (w *Worker) process(queue, consumer string, d amqp091.Delivery) common.Action {
	pld, err := buildPayload(queue, consumer, d)
	if err != nil {
		w.log.Error("payload build failed, rejecting",
			zap.String("queue", queue), zap.Error(err))
		return common.ActionReject
	}

	start := time.Now()

	resCh, err := w.pool.Exec(context.Background(), pld, nil)
	if err != nil {
		w.log.Warn("exec failed", zap.String("queue", queue), zap.Error(err))
		w.metrics.ObserveMessage(consumer, queue, "crash", time.Since(start))
		return common.ActionNack
	}
	res, ok := <-resCh
	elapsed := time.Since(start)

	switch {
	case !ok || res == nil:
		w.log.Warn("empty worker response", zap.String("queue", queue))
		w.metrics.ObserveMessage(consumer, queue, "crash", elapsed)
		return common.ActionNack
	case res.Error() != nil:
		w.log.Warn("worker error", zap.String("queue", queue), zap.Error(res.Error()))
		w.metrics.ObserveMessage(consumer, queue, "crash", elapsed)
		return common.ActionNack
	default:
		action := w.parseResult(queue, res.Payload().Body)
		w.metrics.ObserveMessage(consumer, queue, action.Label(), elapsed)
		return action
	}
}

// parseResult interprets the worker's response body as an action enum.
func (w *Worker) parseResult(queue string, body []byte) common.Action {
	if len(body) == 0 {
		w.log.Warn("worker returned empty body", zap.String("queue", queue))
		return common.ActionNack
	}
	switch body[0] {
	case resultAck:
		return common.ActionAck
	case resultNack:
		return common.ActionNack
	case resultReject:
		return common.ActionReject
	default:
		w.log.Warn("unknown worker result byte",
			zap.String("queue", queue), zap.Uint8("byte", body[0]))
		return common.ActionNack
	}
}

// buildPayload maps the delivery onto a worker payload.
func buildPayload(queue, consumer string, d amqp091.Delivery) (*payload.Payload, error) {
	meta, err := json.Marshal(map[string]any{
		"queue":       queue,
		"consumer":    consumer,
		"headers":     d.Headers,
		"exchange":    d.Exchange,
		"routingKey":  d.RoutingKey,
		"deliveryTag": d.DeliveryTag,
	})
	if err != nil {
		return nil, err
	}
	return &payload.Payload{Context: meta, Body: d.Body}, nil
}
