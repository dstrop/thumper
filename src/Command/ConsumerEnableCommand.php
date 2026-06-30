<?php

declare(strict_types=1);

namespace Thumper\Command;

use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Thumper\ThumperInterface;

#[AsCommand(name: 'thumper:consumer:enable', description: 'Enable a consumer (takes effect on next reset)')]
final class ConsumerEnableCommand extends Command
{
    public function __construct(private readonly ThumperInterface $thumper)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->addArgument('name', InputArgument::REQUIRED, 'Consumer name');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        /** @var string $name */
        $name = $input->getArgument('name');

        $this->thumper->enableConsumer($name);

        $output->writeln("Consumer <info>{$name}</info> enabled.");

        return Command::SUCCESS;
    }
}
