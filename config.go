package thumper

import (
	"github.com/roadrunner-server/config/v4"
	"github.com/roadrunner-server/sdk/v4/pool"
	"os"
)

type Config struct {
	Amqp *AmqpConfig `mapstructure:"amqp"`

	Pool *pool.Config `mapstructure:"pool"`

	Consumers []*ConsumerConfig `mapstructure:"consumers"`
}

type AmqpConfig struct {
	Addr string `mapstructure:"addr"`

	Queue     []*QueueConfig     `mapstructure:"queue"`
	Exchange  []*ExchangeConfig  `mapstructure:"exchange"`
	QueueBind []*QueueBindConfig `mapstructure:"queueBind"`
}

type QueueConfig struct {
	Name       string                 `mapstructure:"name"`
	Durable    bool                   `mapstructure:"durable"`
	AutoDelete bool                   `mapstructure:"autoDelete"`
	Exclusive  bool                   `mapstructure:"exclusive"`
	NoWait     bool                   `mapstructure:"noWait"`
	Args       map[string]interface{} `mapstructure:"args"`
}

type ExchangeConfig struct {
	Name       string                 `mapstructure:"name"`
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

func (c *Config) ExpandEnv() {
	for _, consumer := range c.Consumers {
		consumer.ExpandEnv()
	}

	for _, queue := range c.Amqp.Queue {
		queue.ExpandEnv()
	}

	for _, exchange := range c.Amqp.Exchange {
		exchange.ExpandEnv()
	}

	for _, queueBind := range c.Amqp.QueueBind {
		queueBind.ExpandEnv()
	}
}

func (c *ConsumerConfig) ExpandEnv() {
	c.Queue = config.ExpandVal(c.Queue, os.Getenv)
	c.ConsumerID = config.ExpandVal(c.ConsumerID, os.Getenv)
}

func (c *QueueConfig) ExpandEnv() {
	c.Name = config.ExpandVal(c.Name, os.Getenv)
	for key, value := range c.Args {
		if valueStr, ok := value.(string); ok {
			c.Args[key] = config.ExpandVal(valueStr, os.Getenv)
		}
	}
}

func (c *ExchangeConfig) ExpandEnv() {
	c.Name = config.ExpandVal(c.Name, os.Getenv)
	c.Kind = config.ExpandVal(c.Kind, os.Getenv)
	for key, value := range c.Args {
		if valueStr, ok := value.(string); ok {
			c.Args[key] = config.ExpandVal(valueStr, os.Getenv)
		}
	}
}

func (c *QueueBindConfig) ExpandEnv() {
	c.Queue = config.ExpandVal(c.Queue, os.Getenv)
	c.Exchange = config.ExpandVal(c.Exchange, os.Getenv)
	c.Key = config.ExpandVal(c.Key, os.Getenv)
	for key, value := range c.Args {
		if valueStr, ok := value.(string); ok {
			c.Args[key] = config.ExpandVal(valueStr, os.Getenv)
		}
	}
}
