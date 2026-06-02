package thumper

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dstrop/thumper/amqp"
	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/dstrop/thumper/common"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/pool/state/process"
	"go.uber.org/zap"
)

const pluginName string = "thumper"

type Plugin struct {
	cfg    *Config
	log    *zap.Logger
	server common.Server

	mu sync.RWMutex

	pool common.Pool

	amqpClient *amqp.Client
	consumer   *amqp.Consumer
	publisher  *amqp.Publisher
	cancelCtx  context.CancelFunc

	wp *Worker

	// TODO: add metrics exporter
	//statsExporter *metrics.StatsExporter
	// TODO: implement status.Readiness and status.Checker
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

	p.amqpClient = amqp.NewClient(amqp.Config{
		URL:            p.cfg.Amqp.Addr,
		ReconnectDelay: 3 * time.Second,
	})

	return nil
}

func (p *Plugin) getPublisher() *amqp.Publisher {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.publisher == nil {
		p.publisher = p.amqpClient.NewPublisher(amqp.PublisherOptions{
			ConfirmMode: true,
			PoolSize:    4,
		})
	}

	return p.publisher
}

// Serve serves the svc.
func (p *Plugin) Serve() chan error {
	errCh := make(chan error, 2)

	if p.cfg.Pool == nil || p.cfg.Pool.NumWorkers == 0 || len(p.cfg.Consumers) == 0 {
		return errCh
	}

	// getPublisher acquires p.mu internally — call before we lock below.
	publisher := p.getPublisher()

	ctx, cancel := context.WithCancel(context.Background())

	err := p.declare(ctx, publisher)
	if err != nil {
		cancel()
		p.log.Error("failed to declare amqp entities", zap.Error(err))
		errCh <- err
		return errCh
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	newConsumer := p.amqpClient.NewConsumer()

	p.pool, err = p.server.NewPool(context.Background(), p.cfg.Pool, nil, p.log)
	if err != nil {
		p.log.Error("failed to create pool", zap.Error(err))
		cancel()
		_ = newConsumer.Close()
		errCh <- err
		return errCh
	}

	p.consumer = newConsumer
	p.cancelCtx = cancel
	p.wp = NewWorkerPool(p.pool, int(p.cfg.Pool.NumWorkers), p.log)

	for _, consumer := range p.cfg.Consumers {
		deliveries, _ := p.consumer.Consume(ctx, amqp.ConsumeOptions{
			Queue:       consumer.Queue,
			ConsumerTag: consumer.ConsumerID,
			AutoAck:     false,
			Exclusive:   false,
			NoWait:      false,
			Args:        nil,
			Prefetch:    consumer.Prefetch,
		})
		p.wp.Consume(deliveries, consumer)
	}

	return errCh
}

func (p *Plugin) declare(ctx context.Context, publisher *amqp.Publisher) error {
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
	}

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

	for _, bindQueueConfig := range p.cfg.Amqp.QueueBind {
		p.log.Debug("binding queue", zap.Any("queue", bindQueueConfig.Queue), zap.Any("exchange", bindQueueConfig.Exchange))
		err := publisher.DeclareBinding(ctx, amqp.BindingOptions{
			Queue:      bindQueueConfig.Queue,
			Exchange:   bindQueueConfig.Exchange,
			RoutingKey: bindQueueConfig.Key,
			NoWait:     bindQueueConfig.NoWait,
			Args:       amqp091.Table(bindQueueConfig.Args),
		})
		if err != nil {
			return fmt.Errorf("failed to bind queue %s to exchange %s: %w", bindQueueConfig.Queue, bindQueueConfig.Exchange, err)
		}
	}

	return nil
}

// TODO: we should stop the workers first and then close the client to allow workers to finish their work

func (p *Plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancelCtx != nil {
		p.cancelCtx()
	}

	doneCh := make(chan error, 1)

	go func() {
		if p.wp != nil {
			p.wp.WaitClose()
		}
		var firstErr error
		if p.consumer != nil {
			if err := p.consumer.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if p.publisher != nil {
			if err := p.publisher.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		doneCh <- firstErr
	}()

	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case err = <-doneCh:
	}

	p.cancelCtx = nil
	p.consumer = nil
	p.publisher = nil

	return err
}

// Workers returns slice with the process states for the workers
func (p *Plugin) Workers() []*process.State {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.pool == nil {
		return nil
	}

	workers := p.pool.Workers()

	ps := make([]*process.State, 0, len(workers))
	for i := 0; i < len(workers); i++ {
		state, err := process.WorkerProcessState(workers[i])
		if err != nil {
			return nil
		}
		ps = append(ps, state)
	}

	return ps
}

// Reset destroys the old pool and replaces it with new one, waiting for old pool to die
func (p *Plugin) Reset() error {
	const op = errors.Op("http_plugin_reset")

	p.mu.Lock()
	defer p.mu.Unlock()

	p.log.Info("reset signal was received")

	if p.pool == nil {
		p.log.Info("pool is nil, nothing to reset")
		return nil
	}

	err := p.pool.Reset(context.Background())
	if err != nil {
		return errors.E(op, err)
	}

	p.log.Info("plugin was successfully reset")
	return nil
}

// RPC returns associated rpc service.
func (p *Plugin) RPC() any {
	p.mu.Lock()
	defer p.mu.Unlock()

	return &rpc{
		plugin: p,
	}
}
