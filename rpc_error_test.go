package thumper

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/dstrop/thumper/amqp"
	amqp091 "github.com/rabbitmq/amqp091-go"
)

func TestAMQPRPCError(t *testing.T) {
	unknown := errors.New("unknown error")
	tests := []struct {
		name string
		err  error
		kind string
		code int
	}{
		{name: "nil"},
		{name: "unknown", err: unknown},
		{name: "cancelled", err: context.Canceled},
		{name: "timeout", err: context.DeadlineExceeded, kind: "timeout"},
		{name: "timeout takes precedence", err: fmt.Errorf("%w: %w", context.DeadlineExceeded, amqp091.ErrClosed), kind: "timeout"},
		{name: "nack", err: amqp.ErrNacked, kind: "nack"},
		{name: "connection closed", err: amqp.ErrClosed, kind: "connection"},
		{name: "amqp closed before broker error", err: amqp091.ErrClosed, kind: "connection"},
	}
	for _, code := range []int{200, 311, 312, 313, 403, 404, 405, 406, 320, 501, 504, 541, 599, 600} {
		kind, rpcCode := "amqp", code
		if code == 320 || (code >= 500 && code <= 599) {
			kind, rpcCode = "connection", 0
		}
		tests = append(tests, struct {
			name string
			err  error
			kind string
			code int
		}{fmt.Sprint(code), &amqp091.Error{Code: code, Reason: "broker reason"}, kind, rpcCode})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, wrapped := range []bool{false, true} {
				err := tc.err
				if wrapped && err != nil {
					err = fmt.Errorf("outer: %w", err)
				}
				got := amqpRPCError(err)
				if tc.kind == "" {
					if got != err {
						t.Fatalf("untagged error changed: %v", got)
					}
					continue
				}
				var tagged *rpcError
				if !errors.As(got, &tagged) || tagged.kind != tc.kind || tagged.code != tc.code || !errors.Is(got, tc.err) {
					t.Fatalf("unexpected classification: %v", got)
				}
				reason := err.Error()
				if tc.kind == "amqp" {
					reason = "broker reason"
				}
				want := fmt.Sprintf("[thumper:%s:%d] %s", tc.kind, tc.code, reason)
				if got.Error() != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			}
		})
	}
}
