package thumper

import "fmt"

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
	client, err := r.plugin.getClient()
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.Publish(
		message.Exchange,
		message.Key,
		false,
		false,
		message.ContentType,
		[]byte(message.Message),
		message.Headers,
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
	client, err := r.plugin.getClient()
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.DeclareExchange(
		exchange.Name,
		exchange.Kind,
		exchange.Durable,
		exchange.AutoDelete,
		exchange.Internal,
		exchange.NoWait,
		exchange.Args,
	)
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
	client, err := r.plugin.getClient()
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.DeclareQueue(
		queue.Name,
		queue.Durable,
		queue.AutoDelete,
		queue.Exclusive,
		queue.NoWait,
		queue.Args,
	)
}

type BindQueue struct {
	Queue    string         `msgpack:"alias:queue" json:"queue"`
	Exchange string         `msgpack:"alias:exchange" json:"exchange"`
	Key      string         `msgpack:"alias:key" json:"key"`
	NoWait   bool           `msgpack:"alias:noWait" json:"noWait"`
	Args     map[string]any `msgpack:"alias:args" json:"args"`
}

func (r *rpc) BindQueue(bind *BindQueue, _ *bool) error {
	client, err := r.plugin.getClient()
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.BindQueue(
		bind.Queue,
		bind.Exchange,
		bind.Key,
		bind.NoWait,
		bind.Args,
	)
}
