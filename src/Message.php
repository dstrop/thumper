<?php declare(strict_types=1);

namespace Thumper;

class Message
{
    /**
     * @param array<string, list<string>> $headers
     */
    public function __construct(
        public readonly string $body,
        public readonly array $headers,
        public readonly string $queue,
        public readonly string $exchange,
        public readonly string $routingKey,
        public readonly int $deliveryTag,
    ) {
    }
}
