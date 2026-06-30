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
     * @return StatusResponse
     */
    public function status(): array;

    public function setWorkerCount(int $count): void;

    public function setConsumerConcurrency(string $name, int $concurrency): void;

    public function enableConsumer(string $name): void;

    public function disableConsumer(string $name): void;
}
