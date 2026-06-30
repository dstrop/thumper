package amqp

import (
	"context"
	"errors"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
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

// Consume returns a delivery channel that stays open for the lifetime of ctx, transparently reconnecting.
func (c *Consumer) Consume(ctx context.Context, opts ConsumeOptions) (<-chan amqp.Delivery, error) {
	out := make(chan amqp.Delivery)

	go c.consume(ctx, opts, out)

	return out, nil
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

func (c *Consumer) consume(ctx context.Context, opts ConsumeOptions, out chan<- amqp.Delivery) {
	defer close(out)

	log := c.conn.log.With(
		zap.String("queue", opts.Queue),
		zap.String("consumer_tag", opts.ConsumerTag))

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
				zap.Error(err),
				zap.Duration("retry_in", delay))
			if !wait(ctx, c.conn.done, delay) {
				return
			}
			continue
		}

		attempt = 0 // reset backoff
		log.Info("consumer registered")

	loop:
		for {
			select {
			case msg, ok := <-deliveries:
				if !ok {
					break loop // inner channel died - reconnect
				}
				select {
				case out <- msg:
				case <-ctx.Done():
					break loop
				}
			case <-ctx.Done():
				break loop
			}
		}

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
