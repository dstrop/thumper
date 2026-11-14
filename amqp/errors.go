package amqp

import "errors"

var ErrClosed = errors.New("amqpclient: connection closed")

// ErrNacked is returned when the broker nacks a published message in confirm mode.
var ErrNacked = errors.New("amqpclient: publish nacked by broker")
