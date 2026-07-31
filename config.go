package thumper

import (
	"os"
	"time"

	"github.com/roadrunner-server/config/v5"
	"github.com/roadrunner-server/pool/pool"
)

const defaultDeclareTimeout = 30 * time.Second

type Config struct {
	Amqp *AmqpConfig `mapstructure:"amqp"`

	DisableConsumers bool `mapstructure:"disable_consumers"`

	Pool *pool.Config `mapstructure:"pool"`

	Consumers map[string]*ConsumerConfig `mapstructure:"consumers"`
}

type AmqpConfig struct {
	Addr string `mapstructure:"addr"`

	DeclareTimeout time.Duration `mapstructure:"declare_timeout"`

	Queue    map[string]*QueueConfig    `mapstructure:"queue"`
	Exchange map[string]*ExchangeConfig `mapstructure:"exchange"`
}

type QueueConfig struct {
	Name       string                    `mapstructure:"name"`
	Durable    bool                      `mapstructure:"durable"`
	AutoDelete bool                      `mapstructure:"autoDelete"`
	Exclusive  bool                      `mapstructure:"exclusive"`
	NoWait     bool                      `mapstructure:"noWait"`
	Args       map[string]any            `mapstructure:"args"`
	Bindings   map[string]*BindingConfig `mapstructure:"bindings"`
}

type BindingConfig struct {
	Exchange string         `mapstructure:"exchange"`
	Key      string         `mapstructure:"key"`
	NoWait   bool           `mapstructure:"noWait"`
	Args     map[string]any `mapstructure:"args"`
}

type ExchangeConfig struct {
	Name       string         `mapstructure:"name"`
	Kind       string         `mapstructure:"kind"`
	Durable    bool           `mapstructure:"durable"`
	AutoDelete bool           `mapstructure:"autoDelete"`
	Internal   bool           `mapstructure:"internal"`
	NoWait     bool           `mapstructure:"noWait"`
	Args       map[string]any `mapstructure:"args"`
}

type ConsumerConfig struct {
	Queue   string `mapstructure:"queue"`
	Enabled *bool  `mapstructure:"enabled"`

	Prefetch   int    `mapstructure:"prefetch"`
	Durable    bool   `mapstructure:"durable"`
	ConsumerID string `mapstructure:"consumer_id"`

	Concurrency int `mapstructure:"concurrency"`
}

func (c *Config) InitDefaults() {
	if c.Amqp != nil {
		if c.Amqp.DeclareTimeout <= 0 {
			c.Amqp.DeclareTimeout = defaultDeclareTimeout
		}
		for key, queue := range c.Amqp.Queue {
			if queue.Name == "" {
				queue.Name = key
			}
			for exchangeKey, binding := range queue.Bindings {
				if binding.Exchange == "" {
					binding.Exchange = exchangeKey
				}
			}
		}
		for key, exchange := range c.Amqp.Exchange {
			if exchange.Name == "" {
				exchange.Name = key
			}
		}
	}

	if len(c.Consumers) == 0 {
		c.DisableConsumers = true
	}

	if c.Pool == nil {
		c.Pool = &pool.Config{}
	}
	c.Pool.InitDefaults()

	for name, cc := range c.Consumers {
		cc.InitDefaults(name)
		if c.DisableConsumers {
			cc.Enabled = new(false)
		}
	}
}

func (c *ConsumerConfig) InitDefaults(name string) {
	if c.Queue == "" {
		c.Queue = name
	}
	if c.ConsumerID == "" {
		c.ConsumerID = "thumper." + name
	}
	if c.Enabled == nil {
		c.Enabled = new(true)
	}
	if c.Prefetch == 0 {
		c.Prefetch = 1
	}

}

func (c *Config) ExpandEnv() {
	for _, consumer := range c.Consumers {
		consumer.ExpandEnv()
	}

	if c.Amqp != nil {
		for _, queue := range c.Amqp.Queue {
			queue.ExpandEnv()
		}

		for _, exchange := range c.Amqp.Exchange {
			exchange.ExpandEnv()
		}
	}
}

func (c *ConsumerConfig) ExpandEnv() {
	c.Queue = expandEnv(c.Queue)
	c.ConsumerID = expandEnv(c.ConsumerID)
}

func (c *QueueConfig) ExpandEnv() {
	c.Name = expandEnv(c.Name)
	expandEnvMap(c.Args)
	for _, binding := range c.Bindings {
		binding.ExpandEnv()
	}
}

func (c *BindingConfig) ExpandEnv() {
	c.Exchange = expandEnv(c.Exchange)
	c.Key = expandEnv(c.Key)
	expandEnvMap(c.Args)
}

func (c *ExchangeConfig) ExpandEnv() {
	c.Name = expandEnv(c.Name)
	c.Kind = expandEnv(c.Kind)
	expandEnvMap(c.Args)
}

func expandEnv(s string) string {
	return config.ExpandVal(s, os.Getenv)
}

// expandEnvMap expands environment variables in every string value of args
// in place. Non-string values are left untouched.
func expandEnvMap(args map[string]any) {
	for key, value := range args {
		if valueStr, ok := value.(string); ok {
			args[key] = expandEnv(valueStr)
		}
	}
}
