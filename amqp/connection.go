package amqp

import (
	"context"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type connection struct {
	config Config
	conn   *amqp.Connection // nil while disconnected
	mu     sync.Mutex
	ready  chan struct{} // closed when conn transitions nil→non-nil; replaced each cycle
	closed bool
	done   chan struct{} // closed once when Close() is called
}

func newConnection(config Config) *connection {
	c := &connection{
		config: config,
		ready:  make(chan struct{}),
		done:   make(chan struct{}),
	}
	go c.reconnectLoop()
	return c
}

// newChannel blocks until a live connection is available and returns a fresh
// channel on it. Returns ErrClosed if the connection has been shut down, or
// ctx.Err() if the context expires while waiting.
//
// Channel-level errors (e.g. max channels reached) are returned directly
// without niling the connection — the TCP connection is still alive in that
// case and niling it would permanently strand all waiters.
func (c *connection) newChannel(ctx context.Context) (*amqp.Channel, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, ErrClosed
		}
		if c.conn != nil {
			ch, err := c.conn.Channel()
			c.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return ch, nil
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

func (c *connection) reconnectLoop() {
	delay := c.config.ReconnectDelay
	if delay <= 0 {
		delay = time.Second
	}

	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		conn, err := amqp.DialConfig(c.config.URL, c.config.AMQPConfig)
		if err != nil {
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

		notifyClose := make(chan *amqp.Error, 1)
		conn.NotifyClose(notifyClose)

		select {
		case <-notifyClose:
		case <-c.done:
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
