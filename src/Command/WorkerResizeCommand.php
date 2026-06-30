<?php

declare(strict_types=1);

namespace Thumper\Command;

use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Thumper\ThumperInterface;

#[AsCommand(name: 'thumper:workers:set', description: 'Set the number of PHP worker processes')]
final class WorkerResizeCommand extends Command
{
    public function __construct(private readonly ThumperInterface $thumper)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->addArgument('count', InputArgument::REQUIRED, 'Number of worker processes (minimum 1)');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        /** @var string $count */
        $count = $input->getArgument('count');
        $count = (int) $count;

        if ($count < 1) {
            $output->writeln('<error>Worker count must be at least 1.</error>');
            return Command::FAILURE;
        }

        $this->thumper->setWorkerCount($count);

        $output->writeln("Worker count set to <info>{$count}</info>.");

        return Command::SUCCESS;
    }
}
