package thumper

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/dstrop/thumper/amqp"
	"github.com/dstrop/thumper/common"
	"github.com/dstrop/thumper/consumer"
	"github.com/dstrop/thumper/worker"
	"github.com/prometheus/client_golang/prometheus"
	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/roadrunner-server/api/v4/plugins/v1/status"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/pool/state/process"
	"go.uber.org/zap"
)

const pluginName string = "thumper"

const (
	amqpReconnectDelay = 3 * time.Second
	publisherPoolSize  = 4
)

type Plugin struct {
	cfg    *Config
	log    *zap.Logger
	server common.Server

	mu      sync.RWMutex
	running bool

	pool    common.Pool
	metrics *collector

	amqpClient   *amqp.Client
	connMetrics  *amqp.ConnectionMetrics
	publisher    *amqp.Publisher
	amqpConsumer *amqp.Consumer

	runners map[string]*consumer.Runner
	// cancels holds the cancel func for each currently-running consumer.
	// Cancelling stops both the amqp.Consume goroutine and unblocks the
	// runner's semaphore simultaneously.
	cancels map[string]context.CancelFunc
	// subs holds the live AMQP subscription for each currently-running
	// consumer, used to report per-consumer readiness.
	subs map[string]*amqp.Subscription
	jobs chan common.Job

	wp *worker.Worker
}

func (p *Plugin) Name() string {
	return pluginName
}

func (p *Plugin) Init(cfg common.Configurer, log common.Logger, srv common.Server) error {
	const op = errors.Op("thumper_plugin_init")
	if !cfg.Has(pluginName) {
		return errors.E(op, errors.Disabled)
	}

	p.cfg = new(Config)
	err := cfg.UnmarshalKey(pluginName, p.cfg)
	if err != nil {
		return errors.E(op, err)
	}

	p.cfg.InitDefaults()
	p.cfg.ExpandEnv()

	if p.cfg.Amqp == nil {
		return errors.E(op, errors.Disabled)
	}

	p.log = log.NamedLogger(pluginName)
	p.server = srv

	connMetrics := amqp.NewConnectionMetrics(namespace)

	p.amqpClient = amqp.NewClient(p.cfg.Amqp.Addr, amqpReconnectDelay, connMetrics, p.log)

	p.metrics = newCollector(p)
	p.connMetrics = connMetrics

	p.runners = make(map[string]*consumer.Runner, len(p.cfg.Consumers))
	for name, consumerCfg := range p.cfg.Consumers {
		p.runners[name] = consumer.NewRunner(name, consumerCfg.Queue, consumerCfg.Concurrency, p.log)
		p.metrics.consumerConcurrency.WithLabelValues(name, consumerCfg.Queue).Set(float64(consumerCfg.Concurrency))
	}

	return nil
}

func (p *Plugin) MetricsCollector() []prometheus.Collector {
	return []prometheus.Collector{p.metrics, p.connMetrics}
}

func (p *Plugin) getPublisher() *amqp.Publisher {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.publisher == nil {
		p.publisher = p.amqpClient.NewPublisher(amqp.PublisherOptions{
			ConfirmMode: true,
			PoolSize:    publisherPoolSize,
		})
	}

	return p.publisher
}

func (p *Plugin) Serve() chan error {
	errCh := make(chan error, 2)

	publisher := p.getPublisher()

	ctx := context.Background()

	declareCtx, declareCancel := context.WithTimeout(ctx, p.cfg.Amqp.DeclareTimeout)
	err := p.declare(declareCtx, publisher)
	declareCancel()
	if err != nil {
		p.log.Error("failed to declare amqp entities", zap.Error(err))
		errCh <- err
		return errCh
	}

	if p.cfg.DisableConsumers {
		p.mu.Lock()
		p.running = true
		p.mu.Unlock()

		p.log.Info("consumers disabled, publishing only",
			zap.Int("consumers_configured", len(p.cfg.Consumers)))
		return errCh
	}

	p.mu.Lock()

	p.amqpConsumer = p.amqpClient.NewConsumer()

	p.pool, err = p.server.NewPool(ctx, p.cfg.Pool, nil, p.log)
	if err != nil {
		p.log.Error("failed to create pool", zap.Error(err))
		_ = p.amqpConsumer.Close()
		p.pool = nil
		p.mu.Unlock()
		errCh <- err
		return errCh
	}

	p.jobs = make(chan common.Job)
	p.cancels = make(map[string]context.CancelFunc, len(p.cfg.Consumers))
	p.subs = make(map[string]*amqp.Subscription, len(p.cfg.Consumers))

	p.wp = worker.New(p.pool, int(p.cfg.Pool.NumWorkers), p.jobs, p.log, p.metrics)
	p.wp.Start()

	var consumers []string
	for name, cc := range p.cfg.Consumers {
		if *cc.Enabled {
			consumers = append(consumers, name)
		}
	}

	p.running = true

	p.mu.Unlock()

	p.log.Info("worker pool started", zap.Uint64("workers", p.cfg.Pool.NumWorkers))

	for _, name := range consumers {
		if err := p.startRunner(name); err != nil {
			p.log.Error("failed to start consumer", zap.String("name", name), zap.Error(err))
			continue
		}
	}
	p.log.Info("thumper started",
		zap.Int("consumers_started", len(consumers)),
		zap.Int("consumers_configured", len(p.cfg.Consumers)))

	return errCh
}

// startRunner opens an AMQP subscription for the named consumer and starts its
// runner. Must not be called with p.mu held.
func (p *Plugin) startRunner(name string) error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return fmt.Errorf("consuming is not running")
	}
	r, ok := p.runners[name]
	if !ok {
		p.mu.Unlock()
		return fmt.Errorf("unknown consumer %q", name)
	}
	cc := p.cfg.Consumers[name]
	amqpConsumer := p.amqpConsumer
	p.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	sub, err := amqpConsumer.Consume(ctx, amqp.ConsumeOptions{
		Queue:       cc.Queue,
		ConsumerTag: cc.ConsumerID,
		Prefetch:    cc.Prefetch,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("consume %q: %w", name, err)
	}

	if !r.Start(sub.Deliveries(), p.jobs) {
		cancel() // already running
		p.log.Debug("consumer already running", zap.String("name", name))
		return nil
	}

	p.mu.Lock()
	p.cancels[name] = cancel
	p.subs[name] = sub
	p.mu.Unlock()

	p.log.Info("consumer started",
		zap.String("name", name),
		zap.String("queue", cc.Queue),
		zap.Int("concurrency", cc.Concurrency),
		zap.Int("prefetch", cc.Prefetch))

	return nil
}

// stopRunner stops the named consumer's runner and closes its AMQP channel.
// Must not be called with p.mu held.
// Returns a channel that is closed when the runner has fully stopped.
func (p *Plugin) stopRunner(name string) <-chan struct{} {
	p.mu.Lock()
	consumeCancel := p.cancels[name]
	delete(p.cancels, name)
	delete(p.subs, name)
	r := p.runners[name]
	p.mu.Unlock()

	p.log.Info("stopping consumer", zap.String("name", name))

	runnerDone := r.Stop() // cancel pump; AMQP channel still live

	done := make(chan struct{})
	go func() {
		if runnerDone != nil {
			<-runnerDone // wait until all in-flight acks complete
		}
		if consumeCancel != nil {
			consumeCancel() // now safe to close the AMQP channel
		}
		p.log.Info("consumer stopped", zap.String("name", name))
		close(done)
	}()
	return done
}

func (p *Plugin) declare(ctx context.Context, publisher *amqp.Publisher) error {
	for _, exchangeConfig := range p.cfg.Amqp.Exchange {
		p.log.Debug("declaring exchange", zap.Any("exchange", exchangeConfig))
		err := publisher.DeclareExchange(ctx, amqp.ExchangeOptions{
			Name:       exchangeConfig.Name,
			Kind:       exchangeConfig.Kind,
			Durable:    exchangeConfig.Durable,
			AutoDelete: exchangeConfig.AutoDelete,
			Internal:   exchangeConfig.Internal,
			NoWait:     exchangeConfig.NoWait,
			Args:       amqp091.Table(exchangeConfig.Args),
		})
		if err != nil {
			return fmt.Errorf("failed to declare exchange %s: %w", exchangeConfig.Name, err)
		}
	}

	for _, queueConfig := range p.cfg.Amqp.Queue {
		p.log.Debug("declaring queue", zap.Any("queue", queueConfig))
		_, err := publisher.DeclareQueue(ctx, amqp.QueueOptions{
			Name:       queueConfig.Name,
			Durable:    queueConfig.Durable,
			AutoDelete: queueConfig.AutoDelete,
			Exclusive:  queueConfig.Exclusive,
			NoWait:     queueConfig.NoWait,
			Args:       amqp091.Table(queueConfig.Args),
		})
		if err != nil {
			return fmt.Errorf("failed to declare queue %s: %w", queueConfig.Name, err)
		}

		for _, binding := range queueConfig.Bindings {
			p.log.Debug("binding queue", zap.String("queue", queueConfig.Name), zap.String("exchange", binding.Exchange))
			err = publisher.DeclareBinding(ctx, amqp.BindingOptions{
				Queue:      queueConfig.Name,
				Exchange:   binding.Exchange,
				RoutingKey: binding.Key,
				NoWait:     binding.NoWait,
				Args:       amqp091.Table(binding.Args),
			})
			if err != nil {
				return fmt.Errorf("failed to bind queue %s to exchange %s: %w", queueConfig.Name, binding.Exchange, err)
			}
		}
	}

	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	p.log.Info("stopping thumper plugin")

	p.mu.Lock()
	p.running = false
	running := make([]string, 0, len(p.cancels))
	for name := range p.cancels {
		running = append(running, name)
	}
	p.mu.Unlock()

	dones := make([]<-chan struct{}, 0, len(running))
	for _, name := range running {
		dones = append(dones, p.stopRunner(name))
	}
	for i, d := range dones {
		select {
		case <-d:
		case <-ctx.Done():
			p.log.Warn("stop deadline reached; in-flight will be requeued",
				zap.String("consumer", running[i]))
		}
	}

	if p.wp != nil {
		if err := p.wp.Stop(ctx); err != nil {
			p.log.Warn("worker stop deadline exceeded", zap.Error(err))
		}
	}

	if p.pool != nil {
		p.pool.Destroy(ctx)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.amqpConsumer != nil {
		if err := p.amqpConsumer.Close(); err != nil {
			p.log.Warn("amqp consumer close error", zap.Error(err))
		}
		p.amqpConsumer = nil
	}

	return nil
}

// Workers returns slice with the process states for the workers.
func (p *Plugin) Workers() []*process.State {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.pool == nil {
		return []*process.State{}
	}

	workers := p.pool.Workers()
	ps := make([]*process.State, 0, len(workers))
	for i := range workers {
		state, err := process.WorkerProcessState(workers[i])
		if err != nil {
			p.log.Error("workers state", zap.Error(err))
			return ps
		}
		ps = append(ps, state)
	}

	return ps
}

// hasActiveWorker reports whether the pool has at least one live worker
// (Ready or Working). Must be called with p.mu held.
func (p *Plugin) hasActiveWorker() bool {
	if p.pool == nil {
		return false
	}
	workers := p.pool.Workers()
	for i := range workers {
		if workers[i].State().IsActive() {
			return true
		}
	}
	return false
}

func (p *Plugin) Status() (*status.Status, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.running || (!p.cfg.DisableConsumers && !p.hasActiveWorker()) {
		return &status.Status{Code: http.StatusServiceUnavailable}, nil
	}

	return &status.Status{Code: http.StatusOK}, nil
}

func (p *Plugin) Ready() (*status.Status, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.running || (!p.cfg.DisableConsumers && !p.hasActiveWorker()) {
		return &status.Status{Code: http.StatusServiceUnavailable}, nil
	}

	if p.cfg.DisableConsumers {
		return &status.Status{Code: http.StatusOK}, nil
	}

	// Every enabled consumer must be actively subscribed on the broker.
	for name, cc := range p.cfg.Consumers {
		if !*cc.Enabled {
			continue
		}
		sub, ok := p.subs[name]
		if !ok || !sub.Subscribed() {
			return &status.Status{Code: http.StatusServiceUnavailable}, nil
		}
	}

	return &status.Status{Code: http.StatusOK}, nil
}

// Reset destroys the old pool and replaces it with a new one.
func (p *Plugin) Reset() error {
	const op = errors.Op("thumper_plugin_reset")

	p.mu.Lock()
	defer p.mu.Unlock()

	p.log.Info("reset signal was received")

	if p.pool == nil {
		p.log.Info("pool is nil, nothing to reset")
		return nil
	}

	if err := p.pool.Reset(context.Background()); err != nil {
		return errors.E(op, err)
	}

	p.log.Info("plugin was successfully reset")
	return nil
}

// RPC returns associated rpc service.
func (p *Plugin) RPC() any {
	return &rpc{plugin: p}
}
