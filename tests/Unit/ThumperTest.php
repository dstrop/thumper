<?php

declare(strict_types=1);

namespace Thumper\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Spiral\Goridge\RPC\Exception\ServiceException;
use Spiral\Goridge\RPC\RPCInterface;
use Thumper\Exception\NotFoundException;
use Thumper\Thumper;

final class ThumperTest extends TestCase
{
    #[DataProvider('payloads')]
    public function testTimeoutPayload(string $method, array $arguments, string $rpcMethod, array $payload, ?float $timeout, ?int $nanoseconds): void
    {
        $rpc = $this->createMock(RPCInterface::class);
        $rpc->expects(self::once())->method('withServicePrefix')->with('thumper')->willReturnSelf();
        if ($nanoseconds !== null) {
            $payload['timeout'] = $nanoseconds;
        }
        $rpc->expects(self::once())->method('call')->with($rpcMethod, $payload)->willReturn(true);

        (new Thumper($rpc))->{$method}(...[...$arguments, 'timeout' => $timeout]);
    }

    public static function operations(): iterable
    {
        yield 'publish' => ['publish', ['orders', 'created', 'text/plain', 'body'], 'Publish', [
            'exchange' => 'orders', 'key' => 'created', 'contentType' => 'text/plain', 'body' => 'body',
        ]];
        yield 'exchange' => ['exchangeDeclare', ['orders', 'direct'], 'DeclareExchange', [
            'name' => 'orders', 'kind' => 'direct', 'durable' => false, 'autoDelete' => false, 'internal' => false, 'noWait' => false,
        ]];
        yield 'queue' => ['queueDeclare', ['orders'], 'DeclareQueue', [
            'name' => 'orders', 'durable' => false, 'autoDelete' => false, 'exclusive' => false, 'noWait' => false,
        ]];
        yield 'binding' => ['bindQueue', ['orders', 'events', 'created'], 'BindQueue', [
            'queue' => 'orders', 'exchange' => 'events', 'key' => 'created', 'noWait' => false,
        ]];
        yield 'publish headers' => ['publish', ['orders', 'created', 'text/plain', 'body', ['x-id' => '123']], 'Publish', [
            'exchange' => 'orders', 'key' => 'created', 'contentType' => 'text/plain', 'body' => 'body', 'headers' => ['x-id' => '123'],
        ]];
        yield 'exchange args' => ['exchangeDeclare', ['orders', 'topic', true, true, true, true, ['alternate-exchange' => 'fallback']], 'DeclareExchange', [
            'name' => 'orders', 'kind' => 'topic', 'durable' => true, 'autoDelete' => true, 'internal' => true, 'noWait' => true, 'args' => ['alternate-exchange' => 'fallback'],
        ]];
        yield 'queue args' => ['queueDeclare', ['orders', true, true, true, true, ['x-message-ttl' => 1000]], 'DeclareQueue', [
            'name' => 'orders', 'durable' => true, 'autoDelete' => true, 'exclusive' => true, 'noWait' => true, 'args' => ['x-message-ttl' => 1000],
        ]];
        yield 'binding args' => ['bindQueue', ['orders', 'events', 'created', true, ['x-match' => 'all']], 'BindQueue', [
            'queue' => 'orders', 'exchange' => 'events', 'key' => 'created', 'noWait' => true, 'args' => ['x-match' => 'all'],
        ]];
    }

    public static function payloads(): iterable
    {
        foreach (self::operations() as $name => $operation) {
            foreach (['default' => [null, null], 'seconds' => [2.5, 2_500_000_000], 'fraction' => [0.000001, 1000], 'subnanosecond' => [1e-12, 1]] as $case => [$timeout, $nanoseconds]) {
                yield "{$name} {$case}" => [...$operation, $timeout, $nanoseconds];
            }
        }
    }

    #[DataProvider('operations')]
    public function testOmittedTimeout(string $method, array $arguments, string $rpcMethod, array $payload): void
    {
        $rpc = $this->createMock(RPCInterface::class);
        $rpc->method('withServicePrefix')->willReturnSelf();
        $rpc->expects(self::once())->method('call')->with($rpcMethod, $payload)->willReturn(true);

        (new Thumper($rpc))->{$method}(...$arguments);
    }

    #[DataProvider('invalidTimeouts')]
    public function testInvalidTimeout(string $method, array $arguments, float $timeout): void
    {
        $rpc = $this->createMock(RPCInterface::class);
        $rpc->method('withServicePrefix')->willReturnSelf();
        $rpc->expects(self::never())->method('call');
        $this->expectException(\InvalidArgumentException::class);

        (new Thumper($rpc))->{$method}(...[...$arguments, 'timeout' => $timeout]);
    }

    public static function invalidTimeouts(): iterable
    {
        foreach (self::operations() as $name => [$method, $arguments]) {
            foreach ([0.0, -1.0, \INF, -\INF, \NAN, \PHP_FLOAT_MAX, \PHP_INT_MAX / 1_000_000_000] as $index => $timeout) {
                yield "{$name} {$index}" => [$method, $arguments, $timeout];
            }
        }
    }

    #[DataProvider('operations')]
    public function testServiceErrorIsMapped(string $method, array $arguments): void
    {
        $rpc = $this->createMock(RPCInterface::class);
        $rpc->method('withServicePrefix')->willReturnSelf();
        $rpc->method('call')->willThrowException(new ServiceException("Error '[thumper:amqp:404] missing' on relay"));
        $this->expectException(NotFoundException::class);
        $this->expectExceptionCode(404);

        (new Thumper($rpc))->{$method}(...$arguments);
    }
}
