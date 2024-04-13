<?php declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use Spiral\RoadRunner\Worker;

$worker = new \Thumper\Worker(Worker::create());

while ($message = $worker->waitMessage()) {
    try {
        # process message
        fwrite(STDERR, \Tracy\Dumper::toTerminal($message) . PHP_EOL);
    } catch (\Throwable $e) {
        $worker->respond(\Thumper\Response::Reject);
        fwrite(STDERR, \Tracy\Dumper::toTerminal($e) . PHP_EOL);
        break;
    }

    $worker->respond(\Thumper\Response::Ack);
}

fwrite(STDERR, 'Worker stopped' . PHP_EOL);
