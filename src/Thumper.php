<?php declare(strict_types=1);

namespace Thumper;

use Spiral\Goridge\RPC\RPCInterface;

class Thumper implements ThumperInterface
{
    private const SERVICE_NAME = 'thumper';

    private readonly RPCInterface $rpc;

    public function __construct(RPCInterface $rpc)
    {
        $this->rpc = $rpc->withServicePrefix(self::SERVICE_NAME);
    }

    /**
     * @param array<string, mixed> $headers
     */
    public function publish(
        string $exchange,
        string $key,
        string $contentType,
        string $message,
        array $headers = []
    ): void {
        $payload = \compact('exchange', 'key', 'contentType', 'message');

        foreach (\array_keys($headers) as $key) {
            if (!\is_string($key)) {
                throw new \TypeError('Header keys must be strings');
            }
        }
        if ($headers !== []) {
            $payload['headers'] = $headers;
        }

        $this->rpc->call('Publish', $payload);
    }

    /**
     * @param array<string, mixed> $args
     */
    public function exchangeDeclare(
        string $name,
        string $type,
        bool $durable = false,
        bool $autoDelete = false,
        bool $internal = false,
        bool $noWait = false,
        array $args = []
    ): void {
        $payload = \compact('name', 'type', 'durable', 'autoDelete', 'internal', 'noWait');

        foreach (\array_keys($args) as $key) {
            if (!\is_string($key)) {
                throw new \TypeError('Argument keys must be strings');
            }
        }
        if ($args !== []) {
            $payload['args'] = $args;
        }

        $this->rpc->call('ExchangeDeclare', $payload);
    }

    /**
     * @param array<string, mixed> $args
     */
    public function queueDeclare(
        string $name,
        bool $durable = false,
        bool $autoDelete = false,
        bool $exclusive = false,
        bool $noWait = false,
        array $args = []
    ): void {
        $payload = \compact('name', 'durable', 'autoDelete', 'exclusive', 'noWait');

        foreach (\array_keys($args) as $key) {
            if (!\is_string($key)) {
                throw new \TypeError('Argument keys must be strings');
            }
        }
        if ($args !== []) {
            $payload['args'] = $args;
        }

        $this->rpc->call('QueueDeclare', $payload);
    }

    /**
     * @param array<string, mixed> $args
     */
    public function bindQueue(
        string $queue,
        string $exchange,
        string $key,
        bool $noWait = false,
        array $args = []
    ): void {
        $payload = \compact('queue', 'exchange', 'key', 'noWait');

        foreach (\array_keys($args) as $key) {
            if (!\is_string($key)) {
                throw new \TypeError('Argument keys must be strings');
            }
        }
        if ($args !== []) {
            $payload['args'] = $args;
        }

        $this->rpc->call('BindQueue', $payload);
    }
}
