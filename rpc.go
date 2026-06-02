package thumper

import (
	"context"

	"github.com/dstrop/thumper/amqp"
	amqp091 "github.com/rabbitmq/amqp091-go"
)

// TODO: improve error handling, ideally the client will be able to distinguish between rr and amqp errors

type rpc struct {
	plugin *Plugin
}

type Message struct {
	Exchange string `msgpack:"alias:exchange" json:"exchange"`
	Key      string `msgpack:"alias:key" json:"key"`

	ContentType string `msgpack:"alias:contentType" json:"contentType"`

	Message string         `msgpack:"alias:message" json:"message"`
	Headers map[string]any `msgpack:"alias:headers" json:"headers"`
}

func (r *rpc) Publish(message *Message, _ *bool) error {
	return r.plugin.getPublisher().Publish(
		context.Background(),
		amqp091.Publishing{
			Headers:      amqp091.Table(message.Headers),
			ContentType:  message.ContentType,
			Body:         []byte(message.Message),
			DeliveryMode: amqp091.Persistent,
		},
		amqp.PublishOptions{
			Exchange:   message.Exchange,
			RoutingKey: message.Key,
		},
	)
}

type Exchange struct {
	Name       string         `msgpack:"alias:name" json:"name"`
	Kind       string         `msgpack:"alias:kind" json:"kind"`
	Durable    bool           `msgpack:"alias:durable" json:"durable"`
	AutoDelete bool           `msgpack:"alias:autoDelete" json:"autoDelete"`
	Internal   bool           `msgpack:"alias:internal" json:"internal"`
	NoWait     bool           `msgpack:"alias:noWait" json:"noWait"`
	Args       map[string]any `msgpack:"alias:args" json:"args"`
}

func (r *rpc) DeclareExchange(exchange *Exchange, _ *bool) error {
	return r.plugin.getPublisher().DeclareExchange(context.Background(), amqp.ExchangeOptions{
		Name:       exchange.Name,
		Kind:       exchange.Kind,
		Durable:    exchange.Durable,
		AutoDelete: exchange.AutoDelete,
		Internal:   exchange.Internal,
		NoWait:     exchange.NoWait,
		Args:       amqp091.Table(exchange.Args),
	})
}

type Queue struct {
	Name       string         `msgpack:"alias:name" json:"name"`
	Durable    bool           `msgpack:"alias:durable" json:"durable"`
	AutoDelete bool           `msgpack:"alias:autoDelete" json:"autoDelete"`
	Exclusive  bool           `msgpack:"alias:exclusive" json:"exclusive"`
	NoWait     bool           `msgpack:"alias:noWait" json:"noWait"`
	Args       map[string]any `msgpack:"alias:args" json:"args"`
}

func (r *rpc) DeclareQueue(queue *Queue, _ *bool) error {
	_, err := r.plugin.getPublisher().DeclareQueue(context.Background(), amqp.QueueOptions{
		Name:       queue.Name,
		Durable:    queue.Durable,
		AutoDelete: queue.AutoDelete,
		Exclusive:  queue.Exclusive,
		NoWait:     queue.NoWait,
		Args:       amqp091.Table(queue.Args),
	})
	return err
}

type BindQueue struct {
	Queue    string         `msgpack:"alias:queue" json:"queue"`
	Exchange string         `msgpack:"alias:exchange" json:"exchange"`
	Key      string         `msgpack:"alias:key" json:"key"`
	NoWait   bool           `msgpack:"alias:noWait" json:"noWait"`
	Args     map[string]any `msgpack:"alias:args" json:"args"`
}

func (r *rpc) BindQueue(bind *BindQueue, _ *bool) error {
	return r.plugin.getPublisher().DeclareBinding(context.Background(), amqp.BindingOptions{
		Queue:      bind.Queue,
		Exchange:   bind.Exchange,
		RoutingKey: bind.Key,
		NoWait:     bind.NoWait,
		Args:       amqp091.Table(bind.Args),
	})
}
