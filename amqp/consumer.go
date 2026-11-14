package amqp

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ConsumeOptions configures a single Consume call.
type ConsumeOptions struct {
	Queue       string
	ConsumerTag string
	AutoAck     bool
	Exclusive   bool
	NoWait      bool
	Args        amqp.Table
	Prefetch    int
}

// Consumer wraps a connection and provides a method to consume messages from a queue.
type Consumer struct {
	conn *connection
}

// Subscription is a live consume session.
type Subscription struct {
	deliveries chan amqp.Delivery
	subscribed atomic.Bool
}

// Deliveries returns the channel messages are delivered on.
func (s *Subscription) Deliveries() <-chan amqp.Delivery {
	return s.deliveries
}

// Subscribed reports whether the consumer is currently registered on the broker.
func (s *Subscription) Subscribed() bool {
	return s.subscribed.Load()
}

// Consume starts a Subscription whose delivery channel stays open for the
// lifetime of ctx, transparently reconnecting.
func (c *Consumer) Consume(ctx context.Context, opts ConsumeOptions) (*Subscription, error) {
	sub := &Subscription{deliveries: make(chan amqp.Delivery)}

	go c.consume(ctx, opts, sub)

	return sub, nil
}

// register opens a fresh channel, applies prefetch, and starts consuming.
func (c *Consumer) register(ctx context.Context, opts ConsumeOptions) (*amqp.Channel, <-chan amqp.Delivery, error) {
	amqpChan, err := c.conn.newChannel(ctx)
	if err != nil {
		return nil, nil, err
	}

	if opts.Prefetch > 0 {
		if err := amqpChan.Qos(opts.Prefetch, 0, false); err != nil {
			_ = amqpChan.Close()
			return nil, nil, err
		}
	}

	deliveries, err := amqpChan.Consume(
		opts.Queue,
		opts.ConsumerTag,
		opts.AutoAck,
		opts.Exclusive,
		false, // noLocal - not supported by RabbitMQ
		opts.NoWait,
		opts.Args,
	)
	if err != nil {
		_ = amqpChan.Close()
		return nil, nil, err
	}

	return amqpChan, deliveries, nil
}

func (c *Consumer) consume(ctx context.Context, opts ConsumeOptions, sub *Subscription) {
	defer close(sub.deliveries)

	log := c.conn.log.With(
		slog.String("queue", opts.Queue),
		slog.String("consumer_tag", opts.ConsumerTag))

	attempt := 0

	for {
		amqpChan, deliveries, err := c.register(ctx, opts)
		if err != nil {
			// ErrClosed / ctx cancellation are terminal.
			if errors.Is(err, ErrClosed) || ctx.Err() != nil {
				return
			}
			// Anything else attempts to reconnect the consumer.
			attempt++
			delay := backoff(c.conn.reconnectDelay, attempt)
			log.Warn("consumer setup failed, retrying",
				slog.Any("error", err),
				slog.Duration("retry_in", delay))
			if !wait(ctx, c.conn.done, delay) {
				return
			}
			continue
		}

		attempt = 0 // reset backoff
		sub.subscribed.Store(true)
		log.Info("consumer registered")

	loop:
		for {
			select {
			case msg, ok := <-deliveries:
				if !ok {
					break loop // inner channel died - reconnect
				}
				select {
				case sub.deliveries <- msg:
				case <-ctx.Done():
					break loop
				}
			case <-ctx.Done():
				break loop
			}
		}

		sub.subscribed.Store(false)
		_ = amqpChan.Close()

		if ctx.Err() != nil {
			return
		}

		log.Warn("delivery stream interrupted, re-registering consumer")
	}
}

func (c *Consumer) Close() error {
	return c.conn.close()
}
