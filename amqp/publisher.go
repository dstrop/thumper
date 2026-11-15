package amqp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"
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

// channelPooler abstracts *channelPool so withChannel can be tested.
type channelPooler interface {
	acquire(ctx context.Context) (*pooledChannel, error)
	release(ch *pooledChannel)
	discard(ch *pooledChannel)
}

type pooledChannel struct {
	publisherChannel
	closeErrors <-chan *amqp.Error
	conn        channelConnection // the owning connection, even after reconnect
}

type channelConnection interface {
	IsClosed() bool
}

type publisherChannel interface {
	publish(context.Context, amqp.Publishing, PublishOptions) (confirmation, error)
	NotifyClose(chan *amqp.Error) chan *amqp.Error
	IsClosed() bool
	Close() error
	QueueDeclare(string, bool, bool, bool, bool, amqp.Table) (amqp.Queue, error)
	ExchangeDeclare(string, string, bool, bool, bool, bool, amqp.Table) error
	QueueBind(string, string, string, bool, amqp.Table) error
}

type amqpChannel struct{ *amqp.Channel }

func (ch *amqpChannel) publish(ctx context.Context, msg amqp.Publishing, opts PublishOptions) (confirmation, error) {
	dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, opts.Exchange, opts.RoutingKey, opts.Mandatory, opts.Immediate, msg)
	if dc == nil {
		return nil, err
	}
	return dc, err
}

func newPooledChannel(ch publisherChannel, conn channelConnection) *pooledChannel {
	// shutdown sends to close listeners before resolving pending confirms.
	// Buffer the notification so shutdown never waits for the publisher.
	return &pooledChannel{publisherChannel: ch, closeErrors: ch.NotifyClose(make(chan *amqp.Error, 1)), conn: conn}
}

type channelPool struct {
	idle        chan *pooledChannel // holds only ready-to-use channels
	tokens      chan struct{}       // permission to create a new channel
	conn        *connection
	confirmMode bool
}

func newChannelPool(conn *connection, size int, confirmMode bool) *channelPool {
	tokens := make(chan struct{}, size)
	for range size {
		tokens <- struct{}{}
	}
	return &channelPool{
		idle:        make(chan *pooledChannel, size),
		tokens:      tokens,
		conn:        conn,
		confirmMode: confirmMode,
	}
}

// acquire prefers reusing an idle channel; only creates a new one if none is currently available.
func (p *channelPool) acquire(ctx context.Context) (*pooledChannel, error) {
	select {
	case ch := <-p.idle: // reuse an idle channel
		return ch, nil
	case <-p.tokens: // create a new channel
		ch, conn, err := p.conn.newChannel(ctx)
		if err != nil {
			p.tokens <- struct{}{}
			return nil, err
		}
		pooled := newPooledChannel(&amqpChannel{ch}, conn)
		if p.confirmMode {
			if err := ch.Confirm(false); err != nil {
				_ = ch.Close()
				p.tokens <- struct{}{}
				return nil, err
			}
		}
		return pooled, nil
	case <-ctx.Done(): // context expired before a channel was available
		return nil, ctx.Err()
	}
}

// release returns a healthy channel to the idle pool for reuse.
func (p *channelPool) release(ch *pooledChannel) {
	p.idle <- ch
}

// discard closes ch and frees up a creation permit for the next acquire.
func (p *channelPool) discard(ch *pooledChannel) {
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
func (p *Publisher) withChannel(ctx context.Context, fn func(*pooledChannel) error) error {
	var ch *pooledChannel
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

		var confirmErr *publishConfirmError
		if errors.As(err, &confirmErr) {
			// The frame was sent. Retrying can publish a duplicate message.
			return err
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
			slog.Int("attempt", attempt),
			slog.Duration("backoff", delay),
			slog.Any("error", err))
	}
}

func (p *Publisher) Publish(ctx context.Context, msg amqp.Publishing, opts PublishOptions) error {
	return p.withChannel(ctx, func(ch *pooledChannel) error {
		dc, err := ch.publish(ctx, msg, opts)
		if err != nil || dc == nil {
			return err
		}
		return p.waitConfirm(ctx, ch, dc)
	})
}

// publishConfirmError prevents retries after the publish frame was sent.
type publishConfirmError struct{ error }

func (e *publishConfirmError) Unwrap() error { return e.error }

type confirmation interface {
	WaitContext(context.Context) (bool, error)
}

func (p *Publisher) waitConfirm(ctx context.Context, ch *pooledChannel, dc confirmation) error {
	acked, err := dc.WaitContext(ctx)
	if err != nil {
		return &publishConfirmError{fmt.Errorf("waiting for publisher confirm: message may or may not have reached the broker: %w", err)}
	}
	if acked {
		return nil
	}
	var closeErr *amqp.Error
	closed := false
	select {
	case closeErr = <-ch.closeErrors:
		closed = true
	default:
	}
	connectionClosed := ch.conn != nil && ch.conn.IsClosed()
	if !connectionClosed && closeErr != nil && isChannelError(closeErr.Code) {
		return &publishConfirmError{closeErr}
	}
	if connectionClosed || closed || ch.IsClosed() {
		cause := ErrClosed
		if closeErr != nil {
			cause = fmt.Errorf("%w: %w", ErrClosed, closeErr)
		}
		return &publishConfirmError{fmt.Errorf("waiting for publisher confirm: message may or may not have reached the broker: %w", cause)}
	}
	return &publishConfirmError{ErrNacked}
}

func isChannelError(code int) bool {
	return (code >= 311 && code <= 313) || (code >= 403 && code <= 406)
}

func (p *Publisher) DeclareQueue(ctx context.Context, opts QueueOptions) (amqp.Queue, error) {
	var q amqp.Queue
	err := p.withChannel(ctx, func(ch *pooledChannel) error {
		var e error
		q, e = ch.QueueDeclare(opts.Name, opts.Durable, opts.AutoDelete, opts.Exclusive, opts.NoWait, opts.Args)
		return e
	})
	return q, err
}

func (p *Publisher) DeclareExchange(ctx context.Context, opts ExchangeOptions) error {
	return p.withChannel(ctx, func(ch *pooledChannel) error {
		return ch.ExchangeDeclare(opts.Name, opts.Kind, opts.Durable, opts.AutoDelete, opts.Internal, opts.NoWait, opts.Args)
	})
}

func (p *Publisher) DeclareBinding(ctx context.Context, opts BindingOptions) error {
	return p.withChannel(ctx, func(ch *pooledChannel) error {
		return ch.QueueBind(opts.Queue, opts.RoutingKey, opts.Exchange, opts.NoWait, opts.Args)
	})
}

func (p *Publisher) Close() error {
	return p.conn.close()
}
