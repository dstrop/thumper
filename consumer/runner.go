package consumer

import (
	"context"
	"sync"

	amqp091 "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"

	"github.com/dstrop/thumper/common"
)

// Runner consumes a single queue, emitting each delivery as a Job bounded by a
// per-queue concurrency limit. It may be started and stopped repeatedly.
type Runner struct {
	name  string
	queue string

	sem *sem // per-queue concurrency limiter
	log *zap.Logger

	inflight sync.WaitGroup // unsettled deliveries; Stop waits on it before exit

	mu         sync.Mutex
	cancel     context.CancelFunc // cancels the pump goroutine's context
	activeDone chan struct{}      // non-nil while a run goroutine is active; closed on exit
}

func NewRunner(name, queue string, concurrency int, log *zap.Logger) *Runner {
	return &Runner{
		name:  name,
		queue: queue,
		sem:   newSem(concurrency),
		log:   log,
	}
}

func (r *Runner) Start(deliveries <-chan amqp091.Delivery, out chan<- common.Job) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.activeDone != nil {
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.activeDone = make(chan struct{})

	go r.run(ctx, deliveries, out)

	return true
}

func (r *Runner) Stop() <-chan struct{} {
	r.mu.Lock()
	cancel := r.cancel
	done := r.activeDone
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return done
}

// Resize changes the concurrency limit live. No restart needed.
func (r *Runner) Resize(n int) {
	r.sem.Resize(n)
}

// run drains deliveries until ctx is cancelled or the source closes the channel.
func (r *Runner) run(ctx context.Context, deliveries <-chan amqp091.Delivery, out chan<- common.Job) {
	defer func() {
		r.inflight.Wait()

		r.mu.Lock()
		close(r.activeDone)
		r.cancel = nil
		r.activeDone = nil
		r.mu.Unlock()
	}()

	for {
		// gate intake: do not read the next delivery until a slot is free
		if err := r.sem.Acquire(ctx); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			r.sem.Release()
			return
		case d, ok := <-deliveries:
			if !ok {
				r.sem.Release()
				return
			}
			r.dispatch(ctx, d, out)
		}
	}
}

// dispatch sends one delivery onto the shared channel.
// The send blocks (backpressure) until a reader takes it or ctx is cancelled.
func (r *Runner) dispatch(ctx context.Context, d amqp091.Delivery, out chan<- common.Job) {
	r.inflight.Add(1)
	j := common.Job{
		Queue:    r.queue,
		Consumer: r.name,
		Delivery: d,
		Settle: func(a common.Action) {
			r.settle(d, a)
			r.sem.Release()
			r.inflight.Done()
		},
	}
	select {
	case <-ctx.Done():
		// not handed off: release here; broker requeues on channel close
		r.sem.Release()
		r.inflight.Done()
	case out <- j:
	}
}

func (r *Runner) settle(d amqp091.Delivery, a common.Action) {
	var err error
	switch a {
	case common.ActionAck:
		err = d.Ack(false)
	case common.ActionNack:
		err = d.Nack(false, true)
	case common.ActionReject:
		err = d.Reject(false)
	case common.ActionRetry:
		err = d.Reject(true)
	}
	if err != nil {
		r.log.Warn("settle failed, broker will requeue on channel close",
			zap.String("queue", r.queue),
			zap.Uint64("delivery_tag", d.DeliveryTag),
			zap.Error(err))
	}
}

// Status returns a point-in-time snapshot.
func (r *Runner) Status() Status {
	r.mu.Lock()
	running := r.activeDone != nil
	r.mu.Unlock()
	return Status{
		Name:        r.name,
		Queue:       r.queue,
		Running:     running,
		Concurrency: r.sem.Limit(),
		InFlight:    r.sem.InFlight(),
	}
}

// Status is a point-in-time snapshot of one runner.
type Status struct {
	Name        string
	Queue       string
	Running     bool
	Concurrency int
	InFlight    int
}
