package amqp

import (
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Config struct {
	URL            string
	ReconnectDelay time.Duration
	// TLS, vhost, auth — passed through to the underlying amqp.DialConfig call.
	AMQPConfig amqp.Config
}

type Client struct {
	config Config
}

func NewClient(config Config) *Client {
	return &Client{config: config}
}

// NewConsumer returns a Consumer with its own dedicated TCP connection.
func (c *Client) NewConsumer() *Consumer {
	return &Consumer{conn: newConnection(c.config)}
}

// NewPublisher returns a Publisher with its own dedicated TCP connection.
func (c *Client) NewPublisher(opts PublisherOptions) *Publisher {
	if opts.PoolSize <= 0 {
		opts.PoolSize = 1
	}
	conn := newConnection(c.config)
	return &Publisher{
		conn: conn,
		pool: newChannelPool(conn, opts.PoolSize, opts.ConfirmMode),
		opts: opts,
	}
}
