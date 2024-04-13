<?php declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use Spiral\RoadRunner\Worker;

$worker = new \Thumper\Worker(Worker::create());

$thumper = new \Thumper\Thumper(\Spiral\Goridge\RPC\RPC::create('tcp://127.0.0.1:6001'));

$thumper->publish('', 'queue1', 'text/plain', 'Hello from Thumper Producer!');
echo "Sent message to queue1\n";

$thumper->publish('', 'queue2', 'text/plain', 'Hello from Thumper Producer!', [
    'x-blauh' => 'yes',
]);
echo "Sent message to queue2\n";
