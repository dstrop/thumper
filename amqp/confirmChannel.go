package amqp

import (
	"context"
	amqp "github.com/rabbitmq/amqp091-go"
	"time"
)

type confirmChannel struct {
	ch *amqp.Channel

	confirms chan amqp.Confirmation

	// TODO: consider poolItem struct to separate the channel from the pool
	stored time.Time
}

func (c *confirmChannel) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	return c.ch.PublishWithContext(ctx, exchange, key, mandatory, immediate, msg)
}

func (c *confirmChannel) Confirms() <-chan amqp.Confirmation {
	return c.confirms
}
