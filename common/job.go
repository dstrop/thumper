package common

import amqp091 "github.com/rabbitmq/amqp091-go"

// Action is the settlement decision a worker reaches for a delivery.
type Action int

const (
	// ActionAck settles with basic.ack.
	ActionAck Action = iota
	// ActionNack settles with basic.nack requeue=true (does not count toward delivery-limit on RabbitMQ 4.3+).
	ActionNack
	// ActionReject settles with basic.reject requeue=false (dead-letter or drop).
	ActionReject
	// ActionRetry settles with basic.reject requeue=true (counts toward delivery-limit).
	ActionRetry
)

func (a Action) Label() string {
	switch a {
	case ActionAck:
		return "ack"
	case ActionReject:
		return "reject"
	case ActionNack:
		return "nack"
	case ActionRetry:
		return "retry"
	default:
		return "unknown"
	}
}

// Job is one unit of work handed from a producer to the worker pool.
// Settle resolves the delivery according to the worker's decision.
type Job struct {
	Queue    string
	Consumer string
	Delivery amqp091.Delivery
	Settle   func(Action)
}
