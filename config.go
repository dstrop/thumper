package thumper

import (
	"github.com/roadrunner-server/sdk/v4/pool"
)

type Config struct {
	Amqp *AmqpConfig `mapstructure:"amqp"`

	Pool *pool.Config `mapstructure:"pool"`

	Consumers []*ConsumerConfig `mapstructure:"consumers"`
}

type AmqpConfig struct {
	Addr string `mapstructure:"addr"`

	Queue     map[string]*QueueConfig    `mapstructure:"queue"`
	Exchange  map[string]*ExchangeConfig `mapstructure:"exchange"`
	QueueBind []*QueueBindConfig         `mapstructure:"queueBind"`
}

type QueueConfig struct {
	Durable    bool                   `mapstructure:"durable"`
	AutoDelete bool                   `mapstructure:"autoDelete"`
	Exclusive  bool                   `mapstructure:"exclusive"`
	NoWait     bool                   `mapstructure:"noWait"`
	Args       map[string]interface{} `mapstructure:"args"`
}

type ExchangeConfig struct {
	Kind       string                 `mapstructure:"kind"`
	Durable    bool                   `mapstructure:"durable"`
	AutoDelete bool                   `mapstructure:"autoDelete"`
	Internal   bool                   `mapstructure:"internal"`
	NoWait     bool                   `mapstructure:"noWait"`
	Args       map[string]interface{} `mapstructure:"args"`
}

type QueueBindConfig struct {
	Queue    string                 `mapstructure:"queue"`
	Exchange string                 `mapstructure:"exchange"`
	Key      string                 `mapstructure:"key"`
	NoWait   bool                   `mapstructure:"noWait"`
	Args     map[string]interface{} `mapstructure:"args"`
}

type ConsumerConfig struct {
	Queue string `mapstructure:"queue"`

	Prefetch      int   `mapstructure:"prefetch"`
	Durable       bool  `mapstructure:"durable"`
	RequeueOnFail *bool `mapstructure:"requeue_on_fail"`

	ConsumerID string `mapstructure:"consumer_id"`

	Concurrency int `mapstructure:"concurrency"`
}

func (c *Config) InitDefaults() {
	if c.Pool != nil {
		c.Pool.InitDefaults()
	}

	for _, consumer := range c.Consumers {
		consumer.InitDefaults()
	}
}

func (c *ConsumerConfig) InitDefaults() {
	if c.Prefetch == 0 {
		c.Prefetch = 1
	}

	if c.RequeueOnFail == nil {
		requeueOnFail := true
		c.RequeueOnFail = &requeueOnFail
	}
}
