package amqp

import amqp "github.com/rabbitmq/amqp091-go"

type Delivery struct {
	amqp.Delivery
}

func (d Delivery) Ack(multiple bool) error {
	return d.Delivery.Ack(multiple)
}

func (d Delivery) Nack(multiple, requeue bool) error {
	return d.Delivery.Nack(multiple, requeue)
}

func (d Delivery) Reject(requeue bool) error {
	return d.Delivery.Reject(requeue)
}
