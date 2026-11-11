package amqp

import (
	"context"
	"errors"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// fakeSource is a channelPooler that hands out nil channels (test fns never
// touch the channel) and records how it was driven. acquireErr, when set,
// makes acquire fail.
type fakeSource struct {
	acquireErr   error
	acquireCalls int
	releaseCalls int
	discardCalls int
}

func (f *fakeSource) acquire(ctx context.Context) (*amqp.Channel, error) {
	f.acquireCalls++
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	return nil, nil
}

func (f *fakeSource) release(ch *amqp.Channel) { f.releaseCalls++ }
func (f *fakeSource) discard(ch *amqp.Channel) { f.discardCalls++ }

// newTestPublisher builds a Publisher wired to src and a bare connection (no
// reconnect goroutine). The tiny reconnectDelay keeps backoff waits short.
func newTestPublisher(src channelPooler) *Publisher {
	return &Publisher{
		conn: &connection{
			reconnectDelay: time.Millisecond,
			done:           make(chan struct{}),
			log:            zap.NewNop(),
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
			err := p.withChannel(ctx, func(*amqp.Channel) error {
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
