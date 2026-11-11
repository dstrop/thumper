<?php

declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use Spiral\RoadRunner\Worker;

$worker = new \Thumper\Worker(Worker::create());

$thumper = new \Thumper\Thumper(\Spiral\Goridge\RPC\RPC::create('tcp://127.0.0.1:6001'));

// Optional first argument tells consumer.php how to respond to the queue1 message:
// ack (default), release, retry or reject. Example: php producer.php retry
$response = $argv[1] ?? null;

$thumper->publish('', 'queue1', 'text/plain', 'Hello from Thumper Producer!', $response !== null ? [
    'x-response' => $response,
] : []);
echo 'Sent message to queue1' . ($response !== null ? " (x-response: {$response})" : '') . "\n";

$thumper->publish('', 'queue2', 'text/plain', 'Hello from Thumper Producer!', [
    'x-blauh' => 'yes',
]);
echo "Sent message to queue2\n";
