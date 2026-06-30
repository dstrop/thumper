<?php

declare(strict_types=1);

namespace Thumper\Command;

use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Thumper\ThumperInterface;

#[AsCommand(name: 'thumper:consumer:concurrency', description: 'Set consumer concurrency (takes effect on next reset)')]
final class ConsumerConcurrencyCommand extends Command
{
    public function __construct(private readonly ThumperInterface $thumper)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->addArgument('name', InputArgument::REQUIRED, 'Consumer name');
        $this->addArgument('concurrency', InputArgument::REQUIRED, 'Number of concurrent handlers');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        /** @var string $name */
        $name = $input->getArgument('name');
        /** @var string $concurrency */
        $concurrency = $input->getArgument('concurrency');
        $concurrency = (int) $concurrency;

        if ($concurrency < 1) {
            $output->writeln('<error>Concurrency must be at least 1.</error>');
            return Command::FAILURE;
        }

        $this->thumper->setConsumerConcurrency($name, $concurrency);

        $output->writeln("Consumer <info>{$name}</info> concurrency set to <info>{$concurrency}</info>.");

        return Command::SUCCESS;
    }
}
