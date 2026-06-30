package amqp

import (
	"context"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

type connection struct {
	url            string
	reconnectDelay time.Duration

	role    string
	metrics *ConnectionMetrics
	log     *zap.Logger

	mu sync.Mutex

	conn *amqp.Connection // nil while disconnected

	ready  chan struct{} // closed when conn transitions nil→non-nil; replaced each cycle
	closed bool
	done   chan struct{} // closed once when Close() is called
}

func newConnection(client *Client, role string) *connection {
	c := &connection{
		url:            client.url,
		reconnectDelay: client.reconnectDelay,
		role:           role,
		metrics:        client.metrics,
		log:            client.log.With(zap.String("connection", role)),
		ready:          make(chan struct{}),
		done:           make(chan struct{}),
	}
	go c.reconnectLoop()
	return c
}

// newChannel blocks until a live connection is available and returns a fresh
// channel on it. Returns ErrClosed if the connection has been shut down, or
// ctx.Err() if the context expires while waiting.
//
// Channel-level errors (e.g. max channels reached) on a live connection are
// returned directly without niling the connection — the TCP connection is
// still alive in that case and niling it would permanently strand all waiters.
// If Channel() fails because the connection itself died, we wait for the next
// reconnect instead of surfacing the error.
func (c *connection) newChannel(ctx context.Context) (*amqp.Channel, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, ErrClosed
		}
		if c.conn != nil {
			ch, err := c.conn.Channel()
			if err == nil {
				c.mu.Unlock()
				return ch, nil
			}
			if !c.conn.IsClosed() {
				c.mu.Unlock()
				return nil, err
			}

			c.conn = nil
			c.ready = make(chan struct{})
		}
		// Capture the current ready channel under the lock so we don't miss a
		// close that happens between the Unlock and the select below.
		ready := c.ready
		c.mu.Unlock()

		select {
		case <-ready: // reconnectLoop dialled successfully; loop to recheck
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.done:
			return nil, ErrClosed
		}
	}
}

// retryDelay returns the configured reconnect delay, defaulting to one second.
func (c *connection) retryDelay() time.Duration {
	if c.reconnectDelay > 0 {
		return c.reconnectDelay
	}
	return time.Second
}

// wait sleeps for d and reports whether the wait ran to completion. It returns
// false early if ctx is cancelled or the connection is closed.
func (c *connection) wait(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	case <-c.done:
		return false
	}
}

func (c *connection) reconnectLoop() {
	delay := c.retryDelay()

	var disconnectedAt time.Time

	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		conn, err := amqp.Dial(c.url)
		if err != nil {
			c.metrics.incReconnectAttempts(c.role)
			c.log.Warn("amqp dial failed, retrying",
				zap.Error(err),
				zap.Duration("retry_in", delay))
			select {
			case <-time.After(delay):
			case <-c.done:
				return
			}
			continue
		}

		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			conn.Close()
			return
		}
		c.conn = conn
		close(c.ready)
		c.mu.Unlock()

		if !disconnectedAt.IsZero() {
			downtime := time.Since(disconnectedAt)
			c.metrics.observeReconnectDuration(c.role, downtime)
			c.log.Info("amqp reconnected", zap.Duration("downtime", downtime))
			disconnectedAt = time.Time{}
		} else {
			c.log.Info("amqp connected")
		}
		c.metrics.setConnectionUp(c.role, 1)

		notifyClose := make(chan *amqp.Error, 1)
		conn.NotifyClose(notifyClose)

		select {
		case closeErr := <-notifyClose:
			c.metrics.setConnectionUp(c.role, 0)
			disconnectedAt = time.Now()
			if closeErr != nil {
				c.log.Warn("amqp connection lost", zap.Error(closeErr))
			} else {
				c.log.Info("amqp connection closed by server")
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

		select {
		case <-time.After(delay):
		case <-c.done:
			return
		}
	}
}

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
