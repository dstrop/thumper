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
	ConfirmMode bool // enable publisher confirms on every pool channel
	PoolSize    int  // number of channels in the pool (default: 1)
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

// poolEntry holds a single channel slot inside channelPool.
type poolEntry struct {
	ch *amqp.Channel
}

// channelPool is a fixed-size pool of AMQP channels. The pool owns channel
// creation: callers acquire and discard entries; the pool replaces nil slots
// transparently inside acquire(), keeping the pool size bounded to PoolSize.
type channelPool struct {
	slots       chan *poolEntry // buffered; nil entry = slot needs a fresh channel
	conn        *connection
	confirmMode bool
}

func newChannelPool(conn *connection, size int, confirmMode bool) *channelPool {
	slots := make(chan *poolEntry, size)
	for range size {
		slots <- nil // all slots start empty
	}
	return &channelPool{slots: slots, conn: conn, confirmMode: confirmMode}
}

// acquire blocks until a slot is available. If the slot is nil it calls
// newChannel to fill it before returning.
func (p *channelPool) acquire(ctx context.Context) (*poolEntry, error) {
	select {
	case entry := <-p.slots:
		if entry != nil {
			return entry, nil
		}
		// Nil slot: create a new channel. On failure put the nil slot back so
		// another caller (or a retry) can try again.
		ch, err := p.conn.newChannel(ctx)
		if err != nil {
			p.slots <- nil
			return nil, err
		}
		if p.confirmMode {
			if err := ch.Confirm(false); err != nil {
				ch.Close()
				p.slots <- nil
				return nil, err
			}
		}
		return &poolEntry{ch: ch}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// release returns a healthy entry to the pool.
func (p *channelPool) release(entry *poolEntry) {
	p.slots <- entry
}

// discard closes the entry's channel and puts a nil slot back so the pool
// creates a replacement on the next acquire.
func (p *channelPool) discard(entry *poolEntry) {
	entry.ch.Close()
	p.slots <- nil
}

// Publisher owns a dedicated publish-side connection and a channel pool.
type Publisher struct {
	conn *connection
	pool *channelPool
	opts PublisherOptions
}

// withChannel acquires a pool slot, runs fn, and releases or discards based on
// the result.
//
// Exit immediately (no retry):
//   - fn succeeds
//   - fn returns a server-side *amqp.Error (e.g. PRECONDITION_FAILED) — semantic,
//     retrying would loop forever
//   - ctx expires — returns a wrapped error combining ctx.Err() and the last fn error
//
// Retry (discard channel, get a fresh one):
//   - fn returns a transient error: client-side *amqp.Error (e.g. ErrClosed from a
//     dropped connection) or a plain I/O error — a new channel resolves both
func (p *Publisher) withChannel(ctx context.Context, fn func(*amqp.Channel) error) error {
	var lastErr error
	for {
		entry, err := p.pool.acquire(ctx)
		if err != nil {
			return err
		}
		lastErr = fn(entry.ch)
		if lastErr == nil {
			p.pool.release(entry)
			return nil
		}
		p.pool.discard(entry)
		var amqpErr *amqp.Error
		if errors.As(lastErr, &amqpErr) && amqpErr.Server {
			return lastErr
		}
		if ctx.Err() != nil {
			return fmt.Errorf("%w; last error: %w", ctx.Err(), lastErr)
		}
		p.conn.log.Debug("transient publish error, retrying on a fresh channel",
			zap.Error(lastErr))
	}
}

// Publish sends msg, retrying transparently on channel or connection failures.
// It blocks if all pool slots are busy or the connection is recovering.
// ctx controls the maximum wait time.
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
