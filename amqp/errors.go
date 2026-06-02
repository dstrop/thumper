package amqp

import "errors"

var ErrClosed = errors.New("amqpclient: connection closed")
