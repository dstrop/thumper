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
	Prefetch    int // applied via Qos on every new channel; 0 means no limit
}

// Consumer owns a dedicated consume-side connection.
type Consumer struct {
	conn *connection
}

// Consume returns a delivery channel that remains open for the lifetime of ctx.
// Internally it re-creates the AMQP channel and re-registers the consumer
// after any disruption; the outer channel the caller holds is never closed
// until ctx is cancelled or the Consumer is closed.
func (c *Consumer) Consume(ctx context.Context, opts ConsumeOptions) (<-chan amqp.Delivery, error) {
	out := make(chan amqp.Delivery)

	go c.consume(ctx, opts, out)

	return out, nil
}
func (c *Consumer) consume(ctx context.Context, opts ConsumeOptions, out chan<- amqp.Delivery) {
	defer close(out)

	log := c.conn.log.With(
		zap.String("queue", opts.Queue),
		zap.String("consumer_tag", opts.ConsumerTag))

	// Qos/Consume can fail repeatedly while the connection is alive (e.g. the
	// queue does not exist — NOT_FOUND closes only the channel), so those paths
	// back off instead of immediately grabbing a fresh channel and hammering
	// the broker.
	retryDelay := c.conn.retryDelay()

	for {
		amqpChan, err := c.conn.newChannel(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClosed) {
				log.Warn("consume stopped: channel unavailable", zap.Error(err))
			}
			return
		}

		if opts.Prefetch > 0 {
			if err := amqpChan.Qos(opts.Prefetch, 0, false); err != nil {
				amqpChan.Close()
				log.Warn("qos failed, retrying",
					zap.Error(err),
					zap.Duration("retry_in", retryDelay))
				if !c.conn.wait(ctx, retryDelay) {
					return
				}
				continue
			}
		}

		deliveries, err := amqpChan.Consume(
			opts.Queue,
			opts.ConsumerTag,
			opts.AutoAck,
			opts.Exclusive,
			false, // noLocal — not supported by RabbitMQ
			opts.NoWait,
			opts.Args,
		)
		if err != nil {
			amqpChan.Close()
			log.Warn("consume registration failed, retrying",
				zap.Error(err),
				zap.Duration("retry_in", retryDelay))
			if !c.conn.wait(ctx, retryDelay) {
				return
			}
			continue
		}

		log.Info("consumer registered")

	loop:
		for {
			select {
			case msg, ok := <-deliveries:
				if !ok {
					break loop // inner channel died — reconnect
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

		amqpChan.Close() // best-effort; error ignored

		if ctx.Err() != nil {
			return
		}

		log.Warn("delivery stream interrupted, re-registering consumer")
	}
}

func (c *Consumer) Close() error {
	return c.conn.close()
}
