<?php

declare(strict_types=1);

namespace Thumper\Exception;

use Spiral\Goridge\RPC\Exception\ServiceException;

/**
 * Base class of the errors the plugin reports for RabbitMQ operations.
 *
 * It extends the goridge ServiceException, so code that already catches that one keeps working.
 */
class ThumperException extends ServiceException
{
    /**
     * The plugin prefixes the error text with "[thumper:kind:code] ".
     * Goridge wraps the text as "Error '...' on <relay>".
     */
    private const PATTERN = "/^Error '\\[thumper:([a-z]+):(\\d+)\\] (.*)' on [^']*$/s";

    /**
     * Returns the typed exception for an error tagged by the plugin, or the given exception as is.
     */
    public static function fromServiceException(ServiceException $e): ServiceException
    {
        if ($e instanceof self || \preg_match(self::PATTERN, $e->getMessage(), $m) !== 1) {
            return $e;
        }

        [, $kind, $code, $reason] = $m;
        $code = (int) $code;

        $class = match ($kind) {
            'amqp' => match ($code) {
                403 => AccessRefusedException::class,
                404 => NotFoundException::class,
                405 => ResourceLockedException::class,
                406 => PreconditionFailedException::class,
                default => AmqpException::class,
            },
            'nack' => PublishNackedException::class,
            'timeout' => TimeoutException::class,
            'connection' => ConnectionException::class,
            default => self::class,
        };

        return new $class($reason, $code, $e);
    }
}
