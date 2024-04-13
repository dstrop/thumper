package amqp

import (
	"context"
	"fmt"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
	"time"
)

type Table amqp.Table

type Client struct {
	addr string

	logger *zap.Logger

	conn *amqp.Connection
	mu   sync.Mutex

	chPool *Pool[confirmChannel]

	closed int32
}

func (c *Client) isClosed() bool {
	return atomic.LoadInt32(&c.closed) == 1
}

func (c *Client) Close() error {
	atomic.StoreInt32(&c.closed, 1)

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.conn.Close()
}

func Dial(url string, logger *zap.Logger) (*Client, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}

	c := &Client{
		addr: url,

		logger: logger,

		chPool: NewPool[confirmChannel](),

		conn: conn,
	}

	go c.handleReconnect(url)

	return c, nil
}

func (c *Client) handleReconnect(url string) {
	for {
		reason, ok := <-c.conn.NotifyClose(make(chan *amqp.Error))
		c.logger.Debug("rabbitmq connection closed", zap.NamedError("reason", reason))
		if !ok {
			break
		}

		c.mu.Lock()

		for {
			// TODO: configurable reconnect interval, and consider progressive backoff
			time.Sleep(3 * time.Second)

			if c.isClosed() {
				c.mu.Unlock()
				return
			}

			conn, err := amqp.Dial(url)
			if err != nil {
				c.logger.Debug("rabbitmq reconnect failed", zap.Error(err))
				continue
			}

			c.conn = conn
			c.logger.Debug("rabbitmq reconnect success")
			c.mu.Unlock()
			break
		}
	}
}

func (c *Client) getChannel() (*confirmChannel, error) {
	var ch *confirmChannel
	for {
		ch = c.chPool.Pull()
		if ch == nil {
			break
		}

		// TODO: configurable timeout
		// TODO: soft/hard limit on channel count
		if !ch.ch.IsClosed() && time.Since(ch.stored) < time.Minute {
			return ch, nil
		}

		go ch.ch.Close()
	}

	// TODO: if reconnecting, we should return error or implement optional timeout
	c.mu.Lock()
	defer c.mu.Unlock()

	amqpCh, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// TODO: confirmations should be configurable
	confirms := amqpCh.NotifyPublish(make(chan amqp.Confirmation, 1))
	err = amqpCh.Confirm(false)
	if err != nil {
		return nil, fmt.Errorf("failed to select confirm: %w", err)
	}

	ch = &confirmChannel{
		ch:       amqpCh,
		confirms: confirms,
		stored:   time.Now(),
	}

	return ch, nil
}

func (c *Client) returnChannel(ch *confirmChannel) {
	ch.stored = time.Now()
	c.chPool.Push(ch)
}

func (c *Client) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args Table, prefetch int) (<-chan Delivery, error) {
	con := c.newConsumer(queue, consumer, autoAck, exclusive, noLocal, noWait, args, prefetch)

	d, err := con.Consume()
	if err != nil {
		return nil, fmt.Errorf("failed to consume: %w", err)
	}

	return d, nil
}

func (c *Client) Publish(exchange, key string, mandatory, immediate bool, contentType string, message []byte, headers Table) error {
	ch, err := c.getChannel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer c.returnChannel(ch)

	publishing := amqp.Publishing{
		Headers:      amqp.Table(headers),
		ContentType:  contentType,
		Body:         message,
		DeliveryMode: amqp.Persistent,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	err = ch.PublishWithContext(
		ctx,
		exchange,
		key,
		mandatory,
		immediate,
		publishing,
	)
	if err != nil {
		return fmt.Errorf("publish failed: %w", err)
	}

	select {
	case confirmed, ok := <-ch.Confirms():
		if !ok {
			return fmt.Errorf("confirm channel closed")
		}
		if !confirmed.Ack {
			return fmt.Errorf("message was not acked")
		}
	case <-ctx.Done():
		return fmt.Errorf("publish timeout")
	}

	return nil
}

func (c *Client) DeclareExchange(name, kind string, durable, autoDelete, internal, noWait bool, args Table) error {
	ch, err := c.getChannel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer c.returnChannel(ch)

	return ch.ch.ExchangeDeclare(name, kind, durable, autoDelete, internal, noWait, amqp.Table(args))
}

func (c *Client) DeclareQueue(name string, durable, autoDelete, exclusive, noWait bool, args Table) error {
	ch, err := c.getChannel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer c.returnChannel(ch)

	_, err = ch.ch.QueueDeclare(name, durable, autoDelete, exclusive, noWait, amqp.Table(args))
	return err
}

func (c *Client) BindQueue(queue, exchange, key string, noWait bool, args Table) error {
	ch, err := c.getChannel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer c.returnChannel(ch)

	return ch.ch.QueueBind(queue, key, exchange, noWait, amqp.Table(args))
}
