<?php declare(strict_types=1);

namespace Thumper;

use Spiral\RoadRunner\Payload;
use Spiral\RoadRunner\WorkerInterface as SpiralWorkerInterface;

/**
 * @psalm-type MessageContext = array{
 *     headers: ?array<string,
 *     list<string>>,
 *     queue: string,
 *     exchange: string,
 *     routingKey: string,
 *     deliveryTag: int
 * }
 */
class Worker implements WorkerInterface
{
    public function __construct(
        private readonly SpiralWorkerInterface $worker,
    ) {
    }

    public function getWorker(): SpiralWorkerInterface
    {
        return $this->worker;
    }

    /**
     * @throws \JsonException
     */
    public function waitMessage(): ?Message
    {
        $payload = $this->worker->waitPayload();

        // Termination request
        if ($payload === null || (!$payload->body && !$payload->header)) {
            return null;
        }

        /** @var MessageContext $context */
        $context = \json_decode($payload->header, true, 512, \JSON_THROW_ON_ERROR);

        return new Message(
            body: $payload->body,
            headers: $context['headers'] ?? [],
            queue: $context['queue'],
            exchange: $context['exchange'],
            routingKey: $context['routingKey'],
            deliveryTag: $context['deliveryTag'],
        );
    }

    public function respond(Response $response): void
    {
        $this->worker->respond(new Payload((string)$response->value));
    }
}
