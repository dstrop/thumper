package common

import amqp091 "github.com/rabbitmq/amqp091-go"

// Action is the settlement decision reached for a delivery. It carries no AMQP
// semantics: the producer's Settle closure maps it onto Ack/Nack/Reject and
// picks the requeue flag. It is the contract between a job producer (the
// consumer package) and the worker package, which is why it lives here.
type Action int

const (
	// ActionAck — processed successfully.
	ActionAck Action = iota
	// ActionNack — failed; the producer requeues per its config.
	ActionNack
	// ActionReject — unprocessable; the producer drops without requeue.
	ActionReject
)

func (a Action) Label() string {
	switch a {
	case ActionAck:
		return "ack"
	case ActionReject:
		return "reject"
	case ActionNack:
		return "nack"
	default:
		return "unknown"
	}
}

// Job is one unit of work handed from a producer to the worker pool over the
// shared channel. Settle is a closure owned by the producer: a worker invokes
// it with the decided Action, and it both settles the delivery and releases
// the producer's per-queue slot. The worker therefore needs no knowledge of
// the producer, and routing the result back to the right producer is automatic.
type Job struct {
	Queue    string
	Consumer string
	Delivery amqp091.Delivery
	Settle   func(Action)
}
