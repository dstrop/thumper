<?php

declare(strict_types=1);

namespace Thumper;

enum Response: int
{
    /**
     * Acknowledge the message: `basic.ack`.
     *
     * The message was processed and is removed from the queue.
     */
    case Ack = 0;

    /**
     * Release the message back to the queue: `basic.nack` with `requeue=true`.
     *
     * Use when the message was not processed, for a reason unrelated to the message itself.
     * On RabbitMQ 4.3+ quorum queues this does not increment `delivery-count`
     * and does not count toward `delivery-limit`, so the message can be released indefinitely.
     */
    case Release = 1;

    /**
     * Reject the message without requeue: `basic.reject` with `requeue=false`.
     *
     * The message is dead-lettered if the queue has a dead letter exchange, otherwise it is dropped.
     */
    case Reject = 2;

    /**
     * Retry the message after a failed attempt: `basic.reject` with `requeue=true`.
     *
     * The message is requeued and processing is counted as failed.
     * On quorum queues this increments `delivery-count`; once it exceeds `delivery-limit`
     * the message is dead-lettered or dropped.
     */
    case Retry = 3;

    /**
     * Alias of {@see Response::Release}: `basic.nack` with `requeue=true`.
     */
    public const Nack = self::Release;
}
