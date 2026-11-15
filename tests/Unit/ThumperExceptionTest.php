<?php

declare(strict_types=1);

namespace Thumper\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Spiral\Goridge\RPC\Exception\ServiceException;
use Thumper\Exception\AccessRefusedException;
use Thumper\Exception\AmqpException;
use Thumper\Exception\ConnectionException;
use Thumper\Exception\NotFoundException;
use Thumper\Exception\PreconditionFailedException;
use Thumper\Exception\PublishNackedException;
use Thumper\Exception\ResourceLockedException;
use Thumper\Exception\ThumperException;
use Thumper\Exception\TimeoutException;

final class ThumperExceptionTest extends TestCase
{
    #[DataProvider('taggedErrors')]
    public function testTaggedError(string $kind, int $code, string $class): void
    {
        $reason = "broker's reason\nsecond line";
        $original = new ServiceException("Error '[thumper:{$kind}:{$code}] {$reason}' on tcp://127.0.0.1:6001");
        $exception = ThumperException::fromServiceException($original);

        self::assertInstanceOf($class, $exception);
        self::assertSame($code, $exception->getCode());
        self::assertSame($reason, $exception->getMessage());
        self::assertSame($original, $exception->getPrevious());
        self::assertSame($exception, ThumperException::fromServiceException($exception));
    }

    public static function taggedErrors(): iterable
    {
        yield 'access refused' => ['amqp', 403, AccessRefusedException::class];
        yield 'not found' => ['amqp', 404, NotFoundException::class];
        yield 'resource locked' => ['amqp', 405, ResourceLockedException::class];
        yield 'precondition failed' => ['amqp', 406, PreconditionFailedException::class];
        foreach ([0, 311, 312, 313, 320, 501, 504, 541, 999] as $code) {
            yield "generic amqp {$code}" => ['amqp', $code, AmqpException::class];
        }
        yield 'nack' => ['nack', 0, PublishNackedException::class];
        yield 'timeout' => ['timeout', 0, TimeoutException::class];
        yield 'connection' => ['connection', 0, ConnectionException::class];
        yield 'unknown kind' => ['future', 12, ThumperException::class];
    }

    #[DataProvider('untaggedErrors')]
    public function testUntaggedErrorPassesThrough(string $message): void
    {
        $original = new ServiceException($message, 42);
        self::assertSame($original, ThumperException::fromServiceException($original));
    }

    public static function untaggedErrors(): iterable
    {
        yield ['ordinary error'];
        yield ["Error 'ordinary error' on tcp://127.0.0.1:6001"];
        yield ['[thumper:amqp:404] missing goridge wrapper'];
        yield ["Error '[thumper:amqp:invalid] reason' on relay"];
        yield ["prefix Error '[thumper:amqp:404] reason' on relay"];
    }
}
