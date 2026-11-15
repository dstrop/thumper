<?php

declare(strict_types=1);

require __DIR__ . '/../../vendor/autoload.php';

use Spiral\Goridge\RPC\RPC;
use Thumper\Exception\ConnectionException;
use Thumper\Exception\NotFoundException;
use Thumper\Exception\TimeoutException;
use Thumper\Thumper;

$thumper = new Thumper(RPC::create($argv[1]));
$mode = $argv[2];

if ($mode === 'normal') {
    $thumper->exchangeDeclare('thumper-confirm-check', 'direct', autoDelete: true);
    $thumper->queueDeclare('thumper-confirm-check', durable: true, autoDelete: true);
    $thumper->bindQueue('thumper-confirm-check', 'thumper-confirm-check', 'key');
    $thumper->publish('thumper-confirm-check', 'key', 'text/plain', 'integration');
    echo "Normal publish succeeded\n";
} elseif ($mode === 'missing') {
    try {
        $thumper->publish('thumper-confirm-check-missing', 'key', 'text/plain', 'integration');
        throw new \RuntimeException('Publish to a missing exchange succeeded');
    } catch (NotFoundException $e) {
        if ($e->getCode() !== 404) {
            throw new \RuntimeException('Expected AMQP code 404', previous: $e);
        }
        echo "NotFoundException with code 404\n";
    }
} elseif ($mode === 'outage') {
    try {
        $thumper->publish('thumper-confirm-check', 'key', 'text/plain', 'integration');
        throw new \RuntimeException('Publish succeeded without a confirm');
    } catch (ConnectionException|TimeoutException $e) {
        if (!\str_contains($e->getMessage(), 'may or may not have reached the broker')) {
            throw new \RuntimeException('Missing unknown outcome explanation', previous: $e);
        }
        echo $e::class . ': ' . $e->getMessage() . "\n";
    }
} else {
    throw new \InvalidArgumentException('Unknown integration mode');
}
