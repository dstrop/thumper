<?php

declare(strict_types=1);

namespace Thumper;

/**
 * @phpstan-type ConsumerStatus array{
 *     queue: string,
 *     enabled: bool,
 *     running: bool,
 *     concurrency: int,
 *     prefetch: int,
 * }
 * @phpstan-type WorkerStatus array{
 *     pid: int,
 *     status: int,
 *     numExecs: int,
 *     created: int,
 *     memoryUsage: int,
 *     CPUPercent: float,
 *     command: string,
 *     statusStr: string,
 * }
 * @phpstan-type StatusResponse array{
 *     consumers: array<string, ConsumerStatus>,
 *     workers: list<WorkerStatus>,
 * }
 */
interface ThumperInterface
{
    /**
     * @param array<string, mixed> $headers
     */
    public function publish(
        string $exchange,
        string $key,
        string $contentType,
        string $body,
        array $headers = [],
    ): void;

    /**
     * @return StatusResponse
     */
    public function status(): array;

    public function setWorkerCount(int $count): void;

    public function setConsumerConcurrency(string $name, int $concurrency): void;

    public function enableConsumer(string $name): void;

    public function disableConsumer(string $name): void;

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
    ): void;

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
    ): void;

    /**
     * @param array<string, mixed> $args
     */
    public function bindQueue(
        string $queue,
        string $exchange,
        string $key,
        bool $noWait = false,
        array $args = [],
    ): void;
}
