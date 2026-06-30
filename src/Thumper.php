<?php

declare(strict_types=1);

namespace Thumper;

use Spiral\Goridge\RPC\RPCInterface;

/**
 * @phpstan-import-type StatusResponse from ThumperInterface
 */
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
        string $body,
        array $headers = [],
    ): void {
        $this->assertStringKeys($headers, 'Header');
        $payload = \compact('exchange', 'key', 'contentType', 'body');
        if ($headers !== []) {
            $payload['headers'] = $headers;
        }

        $this->rpc->call('Publish', $payload);
    }

    /**
     * @return StatusResponse
     */
    public function status(): array
    {
        /** @phpstan-ignore cast.string */
        $json = (string) $this->rpc->call('Status', false);

        /** @var StatusResponse $status */
        $status = \json_decode($json, true, flags: \JSON_THROW_ON_ERROR);

        return $status;
    }

    public function setWorkerCount(int $count): void
    {
        $this->rpc->call('SetWorkers', ['workers' => $count]);
    }

    public function setConsumerConcurrency(string $name, int $concurrency): void
    {
        $this->rpc->call('SetConcurrency', \compact('name', 'concurrency'));
    }

    public function enableConsumer(string $name): void
    {
        $this->rpc->call('EnableConsumer', \compact('name'));
    }

    public function disableConsumer(string $name): void
    {
        $this->rpc->call('DisableConsumer', \compact('name'));
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
        array $args = [],
    ): void {
        $this->assertStringKeys($args, 'Argument');
        $payload = \compact('name', 'durable', 'autoDelete', 'internal', 'noWait');
        $payload['kind'] = $type;
        if ($args !== []) {
            $payload['args'] = $args;
        }

        $this->rpc->call('DeclareExchange', $payload);
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
        array $args = [],
    ): void {
        $this->assertStringKeys($args, 'Argument');
        $payload = \compact('name', 'durable', 'autoDelete', 'exclusive', 'noWait');
        if ($args !== []) {
            $payload['args'] = $args;
        }

        $this->rpc->call('DeclareQueue', $payload);
    }

    /**
     * @param array<string, mixed> $args
     */
    public function bindQueue(
        string $queue,
        string $exchange,
        string $key,
        bool $noWait = false,
        array $args = [],
    ): void {
        $this->assertStringKeys($args, 'Argument');
        $payload = \compact('queue', 'exchange', 'key', 'noWait');
        if ($args !== []) {
            $payload['args'] = $args;
        }

        $this->rpc->call('BindQueue', $payload);
    }

    /**
     * @param array<mixed, mixed> $map
     */
    private function assertStringKeys(array $map, string $label): void
    {
        foreach (\array_keys($map) as $key) {
            if (!\is_string($key)) {
                throw new \TypeError("{$label} keys must be strings");
            }
        }
    }
}
