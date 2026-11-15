package amqp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// connection wraps an amqp.Connection and manages reconnecting.
type connection struct {
	url            string
	reconnectDelay time.Duration

	role    string
	metrics *ConnectionMetrics
	log     *slog.Logger

	mu sync.Mutex

	conn *amqp.Connection

	ready  chan struct{}
	closed bool
	done   chan struct{}
}

const defaultReconnectDelay = time.Second

func newConnection(client *Client, role string) *connection {
	reconnectDelay := client.reconnectDelay
	if reconnectDelay <= 0 {
		reconnectDelay = defaultReconnectDelay
	}
	c := &connection{
		url:            client.url,
		reconnectDelay: reconnectDelay,
		role:           role,
		metrics:        client.metrics,
		log:            client.log.With(slog.String("connection", role)),
		ready:          make(chan struct{}),
		done:           make(chan struct{}),
	}
	go c.reconnectLoop()
	return c
}

// newChannel waits for a live connection and returns a fresh channel and its owning connection.
func (c *connection) newChannel(ctx context.Context) (*amqp.Channel, *amqp.Connection, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, nil, ErrClosed
		}
		if c.conn != nil {
			ch, err := c.conn.Channel()
			if err == nil {
				conn := c.conn
				c.mu.Unlock()
				return ch, conn, nil
			}
			if !c.conn.IsClosed() {
				c.mu.Unlock()
				return nil, nil, err
			}

			c.conn = nil
			c.ready = make(chan struct{})
		}

		// Capture the current ready channel under the lock to prevent a race condition.
		ready := c.ready
		c.mu.Unlock()

		select {
		case <-ready: // reconnected
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-c.done:
			return nil, nil, ErrClosed
		}
	}
}

// reconnectLoop runs in a goroutine and manages the connection lifecycle, reconnecting as needed.
func (c *connection) reconnectLoop() {
	disconnectedAt := time.Now() // connecting since goroutine start
	attempt := 0                 // consecutive dial failures; resets on connect

	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		conn, err := amqp.Dial(c.url)
		if err != nil {
			attempt++
			delay := backoff(c.reconnectDelay, attempt)
			c.metrics.incDialFailures(c.role)
			c.log.Warn("amqp dial failed, retrying",
				slog.Any("error", err),
				slog.Duration("retry_in", delay))
			if !wait(context.Background(), c.done, delay) {
				return
			}
			continue
		}
		attempt = 0

		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			_ = conn.Close()
			return
		}
		c.conn = conn
		close(c.ready)
		c.mu.Unlock()

		connectDuration := time.Since(disconnectedAt)
		c.metrics.observeConnectDuration(c.role, connectDuration)
		c.log.Info("amqp connected", slog.Duration("connect_duration", connectDuration))
		c.metrics.setConnectionUp(c.role, 1)

		notifyClose := make(chan *amqp.Error, 1)
		conn.NotifyClose(notifyClose)

		select {
		case closeErr := <-notifyClose:
			c.metrics.setConnectionUp(c.role, 0)
			c.metrics.incConnectionLost(c.role)
			disconnectedAt = time.Now()
			if closeErr != nil {
				c.log.Warn("amqp connection lost", slog.Any("error", closeErr))
			} else {
				c.log.Info("amqp connection closed")
			}
		case <-c.done:
			c.metrics.setConnectionUp(c.role, 0)
			return
		}

		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
			c.ready = make(chan struct{}) // new "not ready" signal for the next dial
		}
		c.mu.Unlock()
	}
}

// close closes the connection and signals the reconnectLoop to exit.
func (c *connection) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	close(c.done) // wakes reconnectLoop sleeps and all newChannel() selects
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
