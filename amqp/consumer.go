package amqp

import (
	"context"

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

	go func() {
		defer close(out)

		for {
			amqpChan, err := c.conn.newChannel(ctx)
			if err != nil {
				return
			}

			if opts.Prefetch > 0 {
				if err := amqpChan.Qos(opts.Prefetch, 0, false); err != nil {
					amqpChan.Close()
					if ctx.Err() != nil {
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
				if ctx.Err() != nil {
					return
				}
				continue
			}

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
		}
	}()

	return out, nil
}

func (c *Consumer) Close() error {
	return c.conn.close()
}
