package amqp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// fakeSource hands out a test channel and records pool operations.
// acquireErr, when set, makes acquire fail.
type fakeSource struct {
	acquireErr   error
	acquireCalls int
	releaseCalls int
	discardCalls int
	channel      *pooledChannel
}

func (f *fakeSource) acquire(ctx context.Context) (*pooledChannel, error) {
	f.acquireCalls++
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	return f.channel, nil
}

func (f *fakeSource) release(ch *pooledChannel) { f.releaseCalls++ }
func (f *fakeSource) discard(ch *pooledChannel) { f.discardCalls++ }

type fakeConfirmation struct {
	wait func(context.Context) (bool, error)
}

func (f fakeConfirmation) WaitContext(ctx context.Context) (bool, error) { return f.wait(ctx) }

type fakeChannel struct {
	*amqpChannel
	confirmation confirmation
	publishErr   error
	closeErrors  chan *amqp.Error
	closed       atomic.Bool
	publishes    int
	message      amqp.Publishing
	options      PublishOptions
}

func (f *fakeChannel) publish(_ context.Context, msg amqp.Publishing, opts PublishOptions) (confirmation, error) {
	f.publishes++
	f.message, f.options = msg, opts
	return f.confirmation, f.publishErr
}

func (f *fakeChannel) NotifyClose(ch chan *amqp.Error) chan *amqp.Error {
	f.closeErrors = ch
	return ch
}

func (f *fakeChannel) IsClosed() bool { return f.closed.Load() }
func (f *fakeChannel) Close() error   { f.closed.Store(true); return nil }

func TestPublishConfirm(t *testing.T) {
	tests := []struct {
		name         string
		ack          bool
		closeCode    int
		closed       bool
		notify       bool
		closeNotify  bool
		parentClosed bool
		waitErr      error
		wantErr      error
		wantReason   string
	}{
		{name: "ack", ack: true},
		{name: "nack", wantErr: ErrNacked},
		{name: "content too large", closeCode: 311},
		{name: "no route", closeCode: 312},
		{name: "no consumers", closeCode: 313},
		{name: "access refused", closeCode: 403},
		{name: "not found", closeCode: 404},
		{name: "resource locked", closeCode: 405},
		{name: "precondition failed", closeCode: 406},
		{name: "connection forced", closeCode: 320, wantErr: ErrClosed, wantReason: "may or may not have reached the broker"},
		{name: "internal error", closeCode: 541, wantErr: ErrClosed, wantReason: "may or may not have reached the broker"},
		{name: "nil close error", notify: true, wantErr: ErrClosed},
		{name: "graceful close", closed: true, closeNotify: true, wantErr: ErrClosed},
		{name: "closed without notification", closed: true, notify: false, wantErr: ErrClosed},
		{name: "connection closed before channel notification", parentClosed: true, wantErr: ErrClosed},
		{name: "connection closed with channel error", parentClosed: true, closeCode: 404, wantErr: ErrClosed},
		{name: "timeout", waitErr: context.DeadlineExceeded, wantErr: context.DeadlineExceeded, wantReason: "may or may not have reached the broker"},
		{name: "cancelled", waitErr: context.Canceled, wantErr: context.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := &fakeChannel{}
			pooled := newPooledChannel(ch, nil)
			parent := &fakeChannel{}
			parent.closed.Store(tc.parentClosed)
			pooled.conn = parent
			if cap(ch.closeErrors) != 1 {
				t.Fatal("NotifyClose must receive a buffered channel")
			}
			src := &fakeSource{channel: pooled}
			p := newTestPublisher(src)
			var closeErr *amqp.Error
			if tc.closeCode != 0 {
				closeErr = &amqp.Error{Code: tc.closeCode, Reason: tc.name, Server: true}
			}
			ch.confirmation = fakeConfirmation{wait: func(context.Context) (bool, error) {
				if src.releaseCalls != 0 || src.discardCalls != 0 {
					t.Fatal("channel returned before confirmation")
				}
				if closeErr != nil || tc.notify {
					ch.closeErrors <- closeErr // Must not block before WaitContext returns.
				}
				if tc.closed {
					ch.closed.Store(true)
				}
				if tc.closeNotify {
					close(ch.closeErrors)
				}
				return tc.ack, tc.waitErr
			}}
			msg := amqp.Publishing{Body: []byte("body"), ContentType: "text/plain"}
			opts := PublishOptions{Exchange: "exchange", RoutingKey: "key", Mandatory: true, Immediate: true}
			err := p.Publish(context.Background(), msg, opts)
			if tc.ack {
				if err != nil || src.releaseCalls != 1 || src.discardCalls != 0 {
					t.Fatalf("ack: err=%v, release=%d, discard=%d", err, src.releaseCalls, src.discardCalls)
				}
			} else {
				if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
					t.Fatalf("got %v, want %v", err, tc.wantErr)
				}
				if tc.closeCode != 0 && tc.wantErr == nil && !errors.Is(err, closeErr) {
					t.Fatalf("got %v, want channel error %v", err, closeErr)
				}
				if err == nil || !strings.Contains(err.Error(), tc.wantReason) || src.discardCalls != 1 || src.releaseCalls != 0 {
					t.Fatalf("failure: err=%v, release=%d, discard=%d", err, src.releaseCalls, src.discardCalls)
				}
			}
			if ch.publishes != 1 || src.acquireCalls != 1 {
				t.Fatal("must not retry after sending the frame")
			}
			if string(ch.message.Body) != "body" || ch.message.ContentType != msg.ContentType || ch.options != opts {
				t.Fatal("publish parameters changed")
			}
		})
	}
}

func TestPublishWithoutConfirms(t *testing.T) {
	ch := &fakeChannel{}
	src := &fakeSource{channel: newPooledChannel(ch, nil)}
	if err := newTestPublisher(src).Publish(context.Background(), amqp.Publishing{}, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if src.releaseCalls != 1 || src.discardCalls != 0 {
		t.Fatal("successful publish without confirms must release the channel")
	}
}

func TestPublishFailureBeforeConfirm(t *testing.T) {
	for _, publishErr := range []error{context.Canceled, &amqp.Error{Code: 406, Reason: "invalid publish"}} {
		t.Run(publishErr.Error(), func(t *testing.T) {
			ch := &fakeChannel{publishErr: publishErr}
			src := &fakeSource{channel: newPooledChannel(ch, nil)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if errors.Is(publishErr, context.Canceled) {
				cancel()
			}
			err := newTestPublisher(src).Publish(ctx, amqp.Publishing{}, PublishOptions{})
			if !errors.Is(err, publishErr) || src.discardCalls != 1 || src.releaseCalls != 0 {
				t.Fatalf("err=%v, release=%d, discard=%d", err, src.releaseCalls, src.discardCalls)
			}
		})
	}
}

func TestPublishConcurrentOwnership(t *testing.T) {
	ch := &fakeChannel{}
	pool := &channelPool{idle: make(chan *pooledChannel, 1), tokens: make(chan struct{})}
	pool.idle <- newPooledChannel(ch, nil)
	p := newTestPublisher(pool)
	waiting := make(chan struct{})
	ack := make(chan struct{})
	ch.confirmation = fakeConfirmation{wait: func(ctx context.Context) (bool, error) {
		close(waiting)
		select {
		case <-ack:
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.Publish(ctx, amqp.Publishing{}, PublishOptions{}) }()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("publish did not start waiting")
	}
	otherCtx, otherCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer otherCancel()
	if _, err := pool.acquire(otherCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("another publish acquired an unconfirmed channel: %v", err)
	}
	close(ack)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := pool.acquire(ctx); err != nil {
		t.Fatalf("acknowledged channel was not released: %v", err)
	}
}

func TestPoolDiscard(t *testing.T) {
	ch := &fakeChannel{}
	pool := &channelPool{tokens: make(chan struct{}, 1)}
	pool.discard(newPooledChannel(ch, nil))
	if !ch.IsClosed() || len(pool.tokens) != 1 {
		t.Fatal("discard must close the channel and restore its permit")
	}
}

// newTestPublisher builds a Publisher wired to src and a bare connection (no
// reconnect goroutine). The tiny reconnectDelay keeps backoff waits short.
func newTestPublisher(src channelPooler) *Publisher {
	return &Publisher{
		conn: &connection{
			reconnectDelay: time.Millisecond,
			done:           make(chan struct{}),
			log:            slog.New(slog.DiscardHandler),
		},
		pool: src,
	}
}

// outcome is how a single pass through withChannel's loop ends: the result is
// handed back to the caller, or fn is retried on a fresh channel.
type outcome int

const (
	returns outcome = iota // withChannel hands the result back to the caller
	retries                // withChannel loops and re-runs fn on a fresh channel
)

func (o outcome) String() string {
	if o == retries {
		return "retries"
	}
	return "returns"
}

func TestWithChannel(t *testing.T) {
	var (
		semanticServer = &amqp.Error{Code: 406, Reason: "PRECONDITION_FAILED", Server: true}
		semanticClient = &amqp.Error{Code: 504, Reason: "channel error", Server: false}
		ioErr          = errors.New("connection reset by peer")
		// a realistic non-recoverable acquire failure: conn.Channel() returns *amqp.Error
		acquireErr = amqp.ErrChannelMax
	)

	// Each case drives a single pass through withChannel's loop. On entry, fn
	// returns fnErr (nil = success) and the ctx/connection may already be done.
	// The pass ends by either returning to the caller or retrying fn.
	tests := []struct {
		name string

		// entry conditions for the pass
		acquireErr error // acquire fails before fn runs
		fnErr      error // fn's result (nil = success)
		cancelCtx  bool  // ctx already cancelled
		closeDone  bool  // connection already shutting down

		// expected end of the pass
		want    outcome // returns vs retries
		wantErr error   // when returns: the error the caller sees (nil = success)
	}{
		{
			name: "success",
			want: returns,
		},
		{
			name:       "acquire fails",
			acquireErr: acquireErr,
			want:       returns,
			wantErr:    acquireErr,
		},
		{
			name:    "server semantic error",
			fnErr:   semanticServer,
			want:    returns,
			wantErr: semanticServer,
		},
		{
			name:    "client semantic error",
			fnErr:   semanticClient,
			want:    returns,
			wantErr: semanticClient,
		},
		{
			name:  "amqp.ErrClosed",
			fnErr: amqp.ErrClosed,
			want:  retries,
		},
		{
			name:  "io error",
			fnErr: ioErr,
			want:  retries,
		},
		{
			name:      "recoverable but ctx cancelled",
			cancelCtx: true,
			fnErr:     ioErr,
			want:      returns,
			wantErr:   context.Canceled,
		},
		{
			name:      "recoverable but conn closing",
			closeDone: true,
			fnErr:     amqp.ErrClosed,
			want:      returns,
			wantErr:   ErrClosed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSource{acquireErr: tc.acquireErr}
			p := newTestPublisher(src)
			if tc.closeDone {
				close(p.conn.done)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if tc.cancelCtx {
				cancel()
			}

			// fn fails once with fnErr, then succeeds, so a retrying pass
			// terminates on the next loop and calls > 1 signals the retry.
			calls := 0
			err := p.withChannel(ctx, func(*pooledChannel) error {
				calls++
				if calls == 1 {
					return tc.fnErr
				}
				return nil
			})

			// No case should reach the ctx deadline; if one does, withChannel
			// spun on retries instead of resolving.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("hit ctx deadline, likely a runaway retry: %v", err)
			}

			got := returns
			if calls > 1 {
				got = retries
			}
			if got != tc.want {
				t.Fatalf("outcome = %s, want %s", got, tc.want)
			}

			if tc.want == returns {
				if tc.wantErr == nil && err != nil {
					t.Fatalf("want nil error, got %v", err)
				}
				if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v in error chain, got %v", tc.wantErr, err)
				}
			}

			// Channel accounting follows from the outcome: a discard for every
			// failed attempt, a release for the one that ultimately succeeds.
			wantRelease, wantDiscard := 0, 0
			switch {
			case tc.acquireErr != nil: // fn never ran
			case tc.want == returns && tc.wantErr == nil: // clean success
				wantRelease = 1
			case tc.want == returns: // failed and returned
				wantDiscard = 1
			case tc.want == retries: // discarded once, then released on retry
				wantRelease, wantDiscard = 1, 1
			}
			if src.releaseCalls != wantRelease {
				t.Errorf("release calls = %d, want %d", src.releaseCalls, wantRelease)
			}
			if src.discardCalls != wantDiscard {
				t.Errorf("discard calls = %d, want %d", src.discardCalls, wantDiscard)
			}
		})
	}
}
