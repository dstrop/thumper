<?php

declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use Symfony\Component\Console\Application;
use Thumper\Command\ConsumerConcurrencyCommand;
use Thumper\Command\ConsumerDisableCommand;
use Thumper\Command\ConsumerEnableCommand;
use Thumper\Command\StatusCommand;
use Thumper\Thumper;

$thumper = new Thumper(\Spiral\Goridge\RPC\RPC::create('tcp://127.0.0.1:6001'));

$app = new Application('thumper', '1.0.0');
$app->addCommands([
    new StatusCommand($thumper),
    new ConsumerEnableCommand($thumper),
    new ConsumerDisableCommand($thumper),
    new ConsumerConcurrencyCommand($thumper),
]);
$app->run();
