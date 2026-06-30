package amqp

import (
	"time"

	"go.uber.org/zap"
)

type Client struct {
	url            string
	reconnectDelay time.Duration
	metrics        *ConnectionMetrics
	log            *zap.Logger
}

// NewClient creates a client factory for consumers and publishers. metrics and
// log are optional; pass nil to disable them.
func NewClient(url string, reconnectDelay time.Duration, metrics *ConnectionMetrics, log *zap.Logger) *Client {
	return &Client{
		url:            url,
		reconnectDelay: reconnectDelay,
		metrics:        metrics,
		log:            log,
	}
}

// NewConsumer returns a Consumer with its own dedicated TCP connection.
func (c *Client) NewConsumer() *Consumer {
	return &Consumer{conn: newConnection(c, "consumer")}
}

// NewPublisher returns a Publisher with its own dedicated TCP connection.
func (c *Client) NewPublisher(opts PublisherOptions) *Publisher {
	if opts.PoolSize <= 0 {
		opts.PoolSize = 1
	}
	conn := newConnection(c, "publisher")
	return &Publisher{
		conn: conn,
		pool: newChannelPool(conn, opts.PoolSize, opts.ConfirmMode),
		opts: opts,
	}
}
