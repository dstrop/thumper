package thumper

import (
	"context"
	"errors"
	"fmt"

	"github.com/dstrop/thumper/amqp"
	amqp091 "github.com/rabbitmq/amqp091-go"
)

// Error kinds understood by the PHP client (Thumper\Exception\ThumperException).
const (
	errKindAMQP       = "amqp"       // the broker refused the operation, code is the AMQP reply code
	errKindNack       = "nack"       // the broker nacked a published message
	errKindTimeout    = "timeout"    // the operation did not finish in time
	errKindConnection = "connection" // no usable connection to the broker
)

// rpcError tags an error with a kind and a code. RoadRunner's RPC carries only the
// error text to the worker, so the tag is a prefix of the message: "[thumper:kind:code] reason".
type rpcError struct {
	kind   string
	code   int
	reason string
	err    error
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("[thumper:%s:%d] %s", e.kind, e.code, e.reason)
}

func (e *rpcError) Unwrap() error {
	return e.err
}

// amqpRPCError classifies an error returned by the publisher. Unknown errors pass through untouched.
func amqpRPCError(err error) error {
	if err == nil {
		return nil
	}

	var amqpErr *amqp091.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &rpcError{kind: errKindTimeout, reason: err.Error(), err: err}
	case errors.Is(err, amqp.ErrNacked):
		return &rpcError{kind: errKindNack, reason: err.Error(), err: err}
	// amqp091.ErrClosed is an *amqp091.Error too, so it has to be matched before the broker errors
	case errors.Is(err, amqp.ErrClosed), errors.Is(err, amqp091.ErrClosed):
		return &rpcError{kind: errKindConnection, reason: err.Error(), err: err}
	case errors.As(err, &amqpErr):
		return &rpcError{kind: errKindAMQP, code: amqpErr.Code, reason: amqpErr.Reason, err: err}
	default:
		return err
	}
}
