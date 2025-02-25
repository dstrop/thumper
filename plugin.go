package thumper

import (
	"context"
	"fmt"
	"github.com/dstrop/thumper/amqp"
	"github.com/dstrop/thumper/common"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/pool/state/process"
	"go.uber.org/zap"
	"sync"
)

const pluginName string = "thumper"

type Plugin struct {
	cfg    *Config
	log    *zap.Logger
	server common.Server

	mu sync.RWMutex

	pool common.Pool

	// TODO: add support for multiple clients
	client *amqp.Client
	wp     *Worker

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

	return nil
}

func (p *Plugin) getClient() (*amqp.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		return p.client, nil
	}

	var err error
	p.client, err = amqp.Dial(p.cfg.Amqp.Addr, p.log)
	if err != nil {
		return nil, fmt.Errorf("failed to dial amqp: %w", err)
	}

	// TODO: consider implementing lazy declaration
	err = p.declare()
	if err != nil {
		return nil, fmt.Errorf("failed to declare amqp entities: %w", err)
	}

	return p.client, nil
}

// Serve serves the svc.
func (p *Plugin) Serve() chan error {
	errCh := make(chan error, 2)

	if p.cfg.Pool == nil || p.cfg.Pool.NumWorkers == 0 || len(p.cfg.Consumers) == 0 {
		return errCh
	}

	client, err := p.getClient()
	if err != nil {
		p.log.Error("failed to get client", zap.Error(err))
		errCh <- err
		return errCh
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.pool, err = p.server.NewPool(context.Background(), p.cfg.Pool, nil, p.log)
	if err != nil {
		p.log.Error("failed to create pool", zap.Error(err))
		errCh <- err
		return errCh
	}

	p.wp = NewWorkerPool(p.pool, int(p.cfg.Pool.NumWorkers), p.log)

	for _, consumer := range p.cfg.Consumers {
		deliveries, err := client.Consume(
			consumer.Queue,
			consumer.ConsumerID,
			false,
			false,
			false,
			false,
			nil,
			consumer.Prefetch,
		)
		if err != nil {
			errCh <- err
			return errCh
		}

		p.wp.Consume(deliveries, consumer)
	}

	return errCh
}

func (p *Plugin) declare() error {
	for _, queueConfig := range p.cfg.Amqp.Queue {
		p.log.Debug("declaring queue", zap.Any("queue", queueConfig))
		err := p.client.DeclareQueue(
			queueConfig.Name,
			queueConfig.Durable,
			queueConfig.AutoDelete,
			queueConfig.Exclusive,
			queueConfig.NoWait,
			queueConfig.Args,
		)
		if err != nil {
			return fmt.Errorf("failed to declare queue %s: %w", queueConfig.Name, err)
		}
	}

	for _, exchangeConfig := range p.cfg.Amqp.Exchange {
		p.log.Debug("declaring exchange", zap.Any("exchange", exchangeConfig))
		err := p.client.DeclareExchange(
			exchangeConfig.Name,
			exchangeConfig.Kind,
			exchangeConfig.Durable,
			exchangeConfig.AutoDelete,
			exchangeConfig.Internal,
			exchangeConfig.NoWait,
			exchangeConfig.Args,
		)
		if err != nil {
			return fmt.Errorf("failed to declare exchange %s: %w", exchangeConfig.Name, err)
		}
	}

	for _, bindQueueConfig := range p.cfg.Amqp.QueueBind {
		p.log.Debug("binding queue", zap.Any("queue", bindQueueConfig.Queue), zap.Any("exchange", bindQueueConfig.Exchange))
		err := p.client.BindQueue(
			bindQueueConfig.Queue,
			bindQueueConfig.Exchange,
			bindQueueConfig.Key,
			bindQueueConfig.NoWait,
			bindQueueConfig.Args,
		)
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

	doneCh := make(chan error, 1)

	go func() {
		err := p.client.Close()
		if err != nil {
			doneCh <- err
			return
		}
		p.wp.WaitClose()
		doneCh <- nil
	}()

	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case err = <-doneCh:
	}

	p.client = nil

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
