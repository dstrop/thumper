<?php declare(strict_types=1);

namespace Thumper;

use Spiral\RoadRunner\WorkerAwareInterface;

interface WorkerInterface extends WorkerAwareInterface
{
    /**
     * Wait for incoming amqp message.
     */
    public function waitMessage(): ?Message;

    /**
     * Send response to the application server.
     *
     * @param Response $response Response to send.
     */
    public function respond(Response $response): void;
}
