package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/roadrunner-server/pool/payload"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"

	"github.com/dstrop/thumper/common"
)

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

type Worker struct {
	pool       common.Pool
	numWorkers int
	log        *zap.Logger
	metrics    Observer
	// execSem gates calls to pool.Exec so at most numWorkers goroutines hold a
	// PHP worker at a time. The +1 readLoop goroutine waits here instead of
	// inside pool.Exec, so pool.Exec never blocks on pool acquisition and the
	// measured elapsed time is pure PHP execution time.
	execSem    *semaphore.Weighted
	stopCtx    context.Context
	stopCancel context.CancelFunc

	jobs    <-chan common.Job
	wg      sync.WaitGroup
	quit    chan struct{}
	running bool
	mu      sync.Mutex
}

func New(pool common.Pool, numWorkers int, jobs <-chan common.Job, log *zap.Logger, metrics Observer) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		pool:       pool,
		numWorkers: numWorkers,
		log:        log,
		metrics:    metrics,
		jobs:       jobs,
		execSem:    semaphore.NewWeighted(int64(numWorkers)),
		stopCtx:    ctx,
		stopCancel: cancel,
	}
}

// Start launches the reader goroutines. A reader in the middle of an execution
// finishes and settles it before exiting, so no in-flight message is stranded.
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

// Stop signals the readers to finish their current message and exit, then
// waits for them. The root must stop the producers first so the readers can
// drain whatever was already handed off. ctx bounds the wait; anything still
// queued is left unacked and the broker requeues it.
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.stopCancel() // unblocks any goroutine waiting on execSem
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

// Resize changes the number of PHP worker processes. Growing is immediate;
// shrinking acquires a semaphore slot first (waits for an idle PHP worker) to
// keep execSem in sync, then removes the worker from the pool.
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
		w.execSem.Release(1) // open a slot for the new PHP worker
		w.wg.Add(1)
		go w.readLoop()
		w.numWorkers++
	}
	for w.numWorkers > n {
		// Acquire a slot before removing: guarantees at least one PHP worker is
		// idle when pool.RemoveWorker runs, and permanently reduces the effective
		// semaphore capacity by 1 (the slot is never released).
		ctx, cancel := context.WithTimeout(context.Background(), removeWorkerTimeout)
		if err := w.execSem.Acquire(ctx, 1); err != nil {
			cancel()
			return fmt.Errorf("wait for idle worker: %w", err)
		}
		cancel()
		if err := w.pool.RemoveWorker(context.Background()); err != nil {
			w.execSem.Release(1) // restore the slot we won't use
			return fmt.Errorf("remove worker: %w", err)
		}
		w.quit <- struct{}{}
		w.numWorkers--
	}
	return nil
}

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
// It acquires execSem before calling pool.Exec so the pool never blocks on
// worker acquisition — elapsed is pure PHP execution time as seen from Go.
func (w *Worker) process(queue, consumer string, d amqp091.Delivery) common.Action {
	pld, err := buildPayload(queue, consumer, d)
	if err != nil {
		w.log.Error("payload build failed, rejecting",
			zap.String("queue", queue), zap.Error(err))
		return common.ActionReject
	}

	if err := w.execSem.Acquire(w.stopCtx, 1); err != nil {
		return common.ActionNack
	}
	start := time.Now()

	resCh, err := w.pool.Exec(context.Background(), pld, nil)
	if err != nil {
		w.execSem.Release(1)
		w.log.Warn("exec failed", zap.String("queue", queue), zap.Error(err))
		w.metrics.ObserveMessage(consumer, queue, "crash", time.Since(start))
		return common.ActionNack
	}
	res, ok := <-resCh
	w.execSem.Release(1)
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

// buildPayload maps the delivery onto a worker payload: Body is the raw
// message body, Context is the JSON-encoded metadata.
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
