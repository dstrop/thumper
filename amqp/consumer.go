package amqp

import (
	"fmt"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
	"time"
)

type consumer struct {
	client *Client
	logger *zap.Logger

	queue       string
	consumerTag string
	autoAck     bool
	exclusive   bool
	noLocal     bool
	noWait      bool
	args        Table
	prefetch    int

	ch *amqp.Channel
	mu sync.Mutex

	// TODO: reevaluate if this is needed
	closed int32
}

func (c *Client) newConsumer(queue, consumerTag string, autoAck, exclusive, noLocal, noWait bool, args Table, prefetch int) *consumer {
	return &consumer{
		client: c,
		logger: c.logger,

		queue:       queue,
		consumerTag: consumerTag,
		autoAck:     autoAck,
		exclusive:   exclusive,
		noLocal:     noLocal,
		noWait:      noWait,
		args:        args,
		prefetch:    prefetch,
	}
}

func (c *consumer) isClosed() bool {
	return atomic.LoadInt32(&c.closed) == 1 || c.client.isClosed()
}

func (c *consumer) Close() error {
	atomic.StoreInt32(&c.closed, 1)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ch != nil {
		err := c.ch.Close()
		if err != nil {
			return fmt.Errorf("failed to close channel: %w", err)
		}
	}

	return nil
}

func (c *consumer) Consume() (<-chan Delivery, error) {
	out := make(chan Delivery)

	in, err := c.dial()
	if err != nil {
		return nil, err
	}

	go c.consume(in, out)
	go c.redial(out)

	return out, nil
}

func (c *consumer) dial() (<-chan amqp.Delivery, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var err error
	c.ch, err = c.client.conn.Channel()
	if err == nil && c.prefetch != 0 {
		err = c.ch.Qos(c.prefetch, 0, false)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	d, err := c.ch.Consume(c.queue, c.consumerTag, c.autoAck, c.exclusive, c.noLocal, c.noWait, amqp.Table(c.args))
	if err != nil {
		return nil, fmt.Errorf("failed to consume: %w", err)
	}

	return d, nil
}

func (c *consumer) consume(in <-chan amqp.Delivery, out chan<- Delivery) {
	for msg := range in {
		out <- Delivery{msg}
	}
	c.logger.Debug("rabbitmq consume channel closed")
}

// TODO: add logging
func (c *consumer) redial(out chan Delivery) {
	defer close(out)

	var err error
	var in <-chan amqp.Delivery

	for {
		closeCh := make(chan *amqp.Error, 1)
		c.ch.NotifyClose(closeCh)

		// nolint:staticcheck // SA4023 err is null when channel is gracefully closed
		err = <-closeCh
		// nolint:staticcheck
		if err == nil {
			return
		}

		in, err = c.dial()
		if err != nil {
			return
		}

		go c.consume(in, out)

		if c.isClosed() {
			break
		}

		go c.ch.Close()
		// TODO: configurable reconnect interval, and consider progressive backoff
		time.Sleep(3 * time.Second)
	}
}
