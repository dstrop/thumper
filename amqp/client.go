package amqp

import (
	"time"

	"go.uber.org/zap"
)

// Client is a factory for creating consumers and publishers with their own dedicated connections.
type Client struct {
	url            string
	reconnectDelay time.Duration
	metrics        *ConnectionMetrics
	log            *zap.Logger
}

func NewClient(url string, reconnectDelay time.Duration, metrics *ConnectionMetrics, log *zap.Logger) *Client {
	return &Client{
		url:            url,
		reconnectDelay: reconnectDelay,
		metrics:        metrics,
		log:            log,
	}
}

// NewConsumer returns a Consumer with its own dedicated connection.
func (c *Client) NewConsumer() *Consumer {
	return &Consumer{conn: newConnection(c, "consumer")}
}

// NewPublisher returns a Publisher with its own dedicated connection.
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
