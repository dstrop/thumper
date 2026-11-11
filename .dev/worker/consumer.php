<?php

declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use Spiral\RoadRunner\Worker;

$worker = new \Thumper\Worker(Worker::create());

fwrite(STDERR, 'Worker started' . PHP_EOL);

// The response is picked by the "x-response" message header (see producer.php), default ack.
function pickResponse(\Thumper\Message $message): \Thumper\Response
{
    $value = $message->headers['x-response'] ?? null;

    return match (is_string($value) ? strtolower($value) : null) {
        'release', 'nack' => \Thumper\Response::Release,
        'retry' => \Thumper\Response::Retry,
        'reject' => \Thumper\Response::Reject,
        default => \Thumper\Response::Ack,
    };
}

while ($message = $worker->waitMessage()) {
    try {
        # process message
        fwrite(STDERR, \Tracy\Dumper::toTerminal($message) . PHP_EOL);
        $response = pickResponse($message);
    } catch (\Throwable $e) {
        $worker->respond(\Thumper\Response::Reject);
        fwrite(STDERR, \Tracy\Dumper::toTerminal($e) . PHP_EOL);
        break;
    }

    fwrite(STDERR, 'Responding ' . $response->name . PHP_EOL);
    $worker->respond($response);
}

fwrite(STDERR, 'Worker stopped' . PHP_EOL);
