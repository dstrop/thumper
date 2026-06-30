package amqp

import (
	"context"
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// PublisherOptions configures a Publisher.
type PublisherOptions struct {
	ConfirmMode bool
	PoolSize    int
}

// PublishOptions controls routing for a single Publish call.
type PublishOptions struct {
	Exchange   string
	RoutingKey string
	Mandatory  bool
	Immediate  bool
}

// QueueOptions mirrors amqp.QueueDeclare parameters.
type QueueOptions struct {
	Name       string
	Durable    bool
	AutoDelete bool
	Exclusive  bool
	NoWait     bool
	Args       amqp.Table
}

// ExchangeOptions mirrors amqp.ExchangeDeclare parameters.
type ExchangeOptions struct {
	Name       string
	Kind       string // "direct", "fanout", "topic", "headers"
	Durable    bool
	AutoDelete bool
	Internal   bool
	NoWait     bool
	Args       amqp.Table
}

// BindingOptions mirrors amqp.QueueBind parameters.
type BindingOptions struct {
	Queue      string
	Exchange   string
	RoutingKey string
	NoWait     bool
	Args       amqp.Table
}

// errNacked is returned when the broker nacks a published message in confirm mode.
var errNacked = errors.New("amqpclient: publish nacked by broker")

// channelPooler abstracts *channelPool so withChannel can be tested.
type channelPooler interface {
	acquire(ctx context.Context) (*amqp.Channel, error)
	release(ch *amqp.Channel)
	discard(ch *amqp.Channel)
}

type channelPool struct {
	idle        chan *amqp.Channel // holds only ready-to-use channels
	tokens      chan struct{}      // permission to create a new channel
	conn        *connection
	confirmMode bool
}

func newChannelPool(conn *connection, size int, confirmMode bool) *channelPool {
	tokens := make(chan struct{}, size)
	for range size {
		tokens <- struct{}{}
	}
	return &channelPool{
		idle:        make(chan *amqp.Channel, size),
		tokens:      tokens,
		conn:        conn,
		confirmMode: confirmMode,
	}
}

// acquire prefers reusing an idle channel; only creates a new one if none is currently available.
func (p *channelPool) acquire(ctx context.Context) (*amqp.Channel, error) {
	select {
	case ch := <-p.idle: // reuse an idle channel
		return ch, nil
	case <-p.tokens: // create a new channel
		ch, err := p.conn.newChannel(ctx)
		if err != nil {
			p.tokens <- struct{}{}
			return nil, err
		}
		if p.confirmMode {
			if err := ch.Confirm(false); err != nil {
				_ = ch.Close()
				p.tokens <- struct{}{}
				return nil, err
			}
		}
		return ch, nil
	case <-ctx.Done(): // context expired before a channel was available
		return nil, ctx.Err()
	}
}

// release returns a healthy channel to the idle pool for reuse.
func (p *channelPool) release(ch *amqp.Channel) {
	p.idle <- ch
}

// discard closes ch and frees up a creation permit for the next acquire.
func (p *channelPool) discard(ch *amqp.Channel) {
	_ = ch.Close()
	p.tokens <- struct{}{}
}

// Publisher owns a dedicated publish-side connection and a channel pool.
type Publisher struct {
	conn *connection
	pool channelPooler
	opts PublisherOptions
}

// withChannel runs fn on a pooled channel, retrying on a fresh one for transport
// failures (with exponential backoff) and returning semantic amqp errors.
func (p *Publisher) withChannel(ctx context.Context, fn func(*amqp.Channel) error) error {
	var ch *amqp.Channel
	var err error
	for attempt := 1; ; attempt++ {
		ch, err = p.pool.acquire(ctx)
		if err == nil {
			err = fn(ch)
			if err == nil {
				p.pool.release(ch)
				return nil
			}
			p.pool.discard(ch)
		}

		var amqpErr *amqp.Error
		if !errors.Is(err, amqp.ErrClosed) && errors.As(err, &amqpErr) {
			return err
		}

		if ctx.Err() != nil {
			return fmt.Errorf("%w; last error: %w", ctx.Err(), err)
		}
		delay := backoff(p.conn.reconnectDelay, attempt)
		if !wait(ctx, p.conn.done, delay) {
			// ctx expired, or the connection is closing (ErrClosed).
			if cause := context.Cause(ctx); cause != nil {
				return fmt.Errorf("%w; last error: %w", cause, err)
			}
			return fmt.Errorf("%w; last error: %w", ErrClosed, err)
		}
		p.conn.log.Debug("recoverable error, retrying on a fresh channel",
			zap.Int("attempt", attempt),
			zap.Duration("backoff", delay),
			zap.Error(err))
	}
}

func (p *Publisher) Publish(ctx context.Context, msg amqp.Publishing, opts PublishOptions) error {
	var dc *amqp.DeferredConfirmation
	err := p.withChannel(ctx, func(ch *amqp.Channel) error {
		var err error
		dc, err = ch.PublishWithDeferredConfirmWithContext(ctx, opts.Exchange, opts.RoutingKey, opts.Mandatory, opts.Immediate, msg)
		return err
	})
	if err != nil {
		return err
	}
	if dc == nil {
		return nil
	}
	acked, err := dc.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !acked {
		return errNacked
	}
	return nil
}

func (p *Publisher) DeclareQueue(ctx context.Context, opts QueueOptions) (amqp.Queue, error) {
	var q amqp.Queue
	err := p.withChannel(ctx, func(ch *amqp.Channel) error {
		var e error
		q, e = ch.QueueDeclare(opts.Name, opts.Durable, opts.AutoDelete, opts.Exclusive, opts.NoWait, opts.Args)
		return e
	})
	return q, err
}

func (p *Publisher) DeclareExchange(ctx context.Context, opts ExchangeOptions) error {
	return p.withChannel(ctx, func(ch *amqp.Channel) error {
		return ch.ExchangeDeclare(opts.Name, opts.Kind, opts.Durable, opts.AutoDelete, opts.Internal, opts.NoWait, opts.Args)
	})
}

func (p *Publisher) DeclareBinding(ctx context.Context, opts BindingOptions) error {
	return p.withChannel(ctx, func(ch *amqp.Channel) error {
		return ch.QueueBind(opts.Queue, opts.RoutingKey, opts.Exchange, opts.NoWait, opts.Args)
	})
}

func (p *Publisher) Close() error {
	return p.conn.close()
}
