package thumper

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/dstrop/thumper/amqp"
	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/roadrunner-server/pool/v2/state/process"
)

const defaultRPCTimeout = 30 * time.Second

type rpc struct {
	plugin *Plugin
}

func rpcCtx(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = defaultRPCTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

type Message struct {
	Exchange string `msgpack:"alias:exchange" json:"exchange"`
	Key      string `msgpack:"alias:key" json:"key"`

	ContentType string `msgpack:"alias:contentType" json:"contentType"`

	Body    string         `msgpack:"alias:body" json:"body"`
	Headers map[string]any `msgpack:"alias:headers" json:"headers"`
	Timeout time.Duration  `msgpack:"alias:timeout" json:"timeout"`
}

func (r *rpc) Publish(message *Message, _ *bool) error {
	ctx, cancel := rpcCtx(message.Timeout)
	defer cancel()
	return amqpRPCError(r.plugin.getPublisher().Publish(
		ctx,
		amqp091.Publishing{
			Headers:      amqp091.Table(message.Headers),
			ContentType:  message.ContentType,
			Body:         []byte(message.Body),
			DeliveryMode: amqp091.Persistent,
		},
		amqp.PublishOptions{
			Exchange:   message.Exchange,
			RoutingKey: message.Key,
		},
	))
}

type Exchange struct {
	Name       string         `msgpack:"alias:name" json:"name"`
	Kind       string         `msgpack:"alias:kind" json:"kind"`
	Durable    bool           `msgpack:"alias:durable" json:"durable"`
	AutoDelete bool           `msgpack:"alias:autoDelete" json:"autoDelete"`
	Internal   bool           `msgpack:"alias:internal" json:"internal"`
	NoWait     bool           `msgpack:"alias:noWait" json:"noWait"`
	Args       map[string]any `msgpack:"alias:args" json:"args"`
	Timeout    time.Duration  `msgpack:"alias:timeout" json:"timeout"`
}

func (r *rpc) DeclareExchange(exchange *Exchange, _ *bool) error {
	ctx, cancel := rpcCtx(exchange.Timeout)
	defer cancel()
	return amqpRPCError(r.plugin.getPublisher().DeclareExchange(ctx, amqp.ExchangeOptions{
		Name:       exchange.Name,
		Kind:       exchange.Kind,
		Durable:    exchange.Durable,
		AutoDelete: exchange.AutoDelete,
		Internal:   exchange.Internal,
		NoWait:     exchange.NoWait,
		Args:       amqp091.Table(exchange.Args),
	}))
}

type Queue struct {
	Name       string         `msgpack:"alias:name" json:"name"`
	Durable    bool           `msgpack:"alias:durable" json:"durable"`
	AutoDelete bool           `msgpack:"alias:autoDelete" json:"autoDelete"`
	Exclusive  bool           `msgpack:"alias:exclusive" json:"exclusive"`
	NoWait     bool           `msgpack:"alias:noWait" json:"noWait"`
	Args       map[string]any `msgpack:"alias:args" json:"args"`
	Timeout    time.Duration  `msgpack:"alias:timeout" json:"timeout"`
}

func (r *rpc) DeclareQueue(queue *Queue, _ *bool) error {
	ctx, cancel := rpcCtx(queue.Timeout)
	defer cancel()
	_, err := r.plugin.getPublisher().DeclareQueue(ctx, amqp.QueueOptions{
		Name:       queue.Name,
		Durable:    queue.Durable,
		AutoDelete: queue.AutoDelete,
		Exclusive:  queue.Exclusive,
		NoWait:     queue.NoWait,
		Args:       amqp091.Table(queue.Args),
	})
	return amqpRPCError(err)
}

type BindQueue struct {
	Queue    string         `msgpack:"alias:queue" json:"queue"`
	Exchange string         `msgpack:"alias:exchange" json:"exchange"`
	Key      string         `msgpack:"alias:key" json:"key"`
	NoWait   bool           `msgpack:"alias:noWait" json:"noWait"`
	Args     map[string]any `msgpack:"alias:args" json:"args"`
	Timeout  time.Duration  `msgpack:"alias:timeout" json:"timeout"`
}

func (r *rpc) BindQueue(bind *BindQueue, _ *bool) error {
	ctx, cancel := rpcCtx(bind.Timeout)
	defer cancel()
	return amqpRPCError(r.plugin.getPublisher().DeclareBinding(ctx, amqp.BindingOptions{
		Queue:      bind.Queue,
		Exchange:   bind.Exchange,
		RoutingKey: bind.Key,
		NoWait:     bind.NoWait,
		Args:       amqp091.Table(bind.Args),
	}))
}

type ConsumerStatus struct {
	Queue       string `json:"queue"`
	Enabled     bool   `json:"enabled"`
	Running     bool   `json:"running"`
	Concurrency int    `json:"concurrency"`
	Prefetch    int    `json:"prefetch"`
}

type StatusResponse struct {
	Consumers map[string]ConsumerStatus `json:"consumers"`
	Workers   []*process.State          `json:"workers"`
}

func (r *rpc) Status(_ *bool, out *string) error {
	r.plugin.mu.RLock()
	consumers := make(map[string]ConsumerStatus, len(r.plugin.runners))
	for name, runner := range r.plugin.runners {
		s := runner.Status()
		cc := r.plugin.cfg.Consumers[name]
		consumers[name] = ConsumerStatus{
			Queue:       s.Queue,
			Enabled:     *cc.Enabled,
			Running:     s.Running,
			Concurrency: s.Concurrency,
			Prefetch:    cc.Prefetch,
		}
	}
	r.plugin.mu.RUnlock()

	data, err := json.Marshal(StatusResponse{
		Consumers: consumers,
		Workers:   r.plugin.Workers(),
	})
	if err != nil {
		return err
	}
	*out = string(data)
	return nil
}

type SetConcurrencyRequest struct {
	Name        string `msgpack:"alias:name"`
	Concurrency int    `msgpack:"alias:concurrency"`
}

func (r *rpc) SetConcurrency(req *SetConcurrencyRequest, _ *bool) error {
	r.plugin.mu.Lock()
	runner, ok := r.plugin.runners[req.Name]
	if ok {
		r.plugin.cfg.Consumers[req.Name].Concurrency = req.Concurrency
	}
	r.plugin.mu.Unlock()

	if !ok {
		return fmt.Errorf("consumer %q not found", req.Name)
	}
	runner.Resize(req.Concurrency)
	r.plugin.metrics.consumerConcurrency.WithLabelValues(req.Name, runner.Status().Queue).Set(float64(req.Concurrency))
	r.plugin.log.Info("consumer concurrency changed via rpc",
		slog.String("name", req.Name),
		slog.Int("concurrency", req.Concurrency))
	return nil
}

type SetWorkersRequest struct {
	Workers int `msgpack:"alias:workers"`
}

func (r *rpc) SetWorkers(req *SetWorkersRequest, _ *bool) error {
	if req.Workers < 1 {
		return fmt.Errorf("workers must be at least 1")
	}
	r.plugin.mu.RLock()
	wp := r.plugin.wp
	r.plugin.mu.RUnlock()
	if wp == nil {
		return fmt.Errorf("worker pool not started")
	}
	if err := wp.Resize(req.Workers); err != nil {
		return err
	}
	r.plugin.log.Info("worker count changed via rpc", slog.Int("workers", req.Workers))
	return nil
}

type ConsumerRequest struct {
	Name string `msgpack:"alias:name"`
}

// setEnabled updates the persisted enabled flag for a consumer and reports
// whether the consumer exists. It must be called without p.mu held.
func (r *rpc) setEnabled(name string, enabled bool) error {
	r.plugin.mu.Lock()
	defer r.plugin.mu.Unlock()
	if _, ok := r.plugin.runners[name]; !ok {
		return fmt.Errorf("consumer %q not found", name)
	}
	if enabled && r.plugin.cfg.DisableConsumers {
		return fmt.Errorf("consuming is disabled for this instance (disable_consumers)")
	}
	r.plugin.cfg.Consumers[name].Enabled = &enabled
	return nil
}

func (r *rpc) EnableConsumer(req *ConsumerRequest, _ *bool) error {
	if err := r.setEnabled(req.Name, true); err != nil {
		return err
	}
	r.plugin.log.Info("enabling consumer via rpc", slog.String("name", req.Name))
	return r.plugin.startRunner(req.Name)
}

func (r *rpc) DisableConsumer(req *ConsumerRequest, _ *bool) error {
	if err := r.setEnabled(req.Name, false); err != nil {
		return err
	}
	r.plugin.log.Info("disabling consumer via rpc", slog.String("name", req.Name))
	if d := r.plugin.stopRunner(req.Name); d != nil {
		<-d
	}
	return nil
}
