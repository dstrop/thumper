<?php

declare(strict_types=1);

namespace Thumper\Command;

use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Thumper\ThumperInterface;

#[AsCommand(name: 'thumper:consumer:disable', description: 'Disable a consumer (takes effect on next reset)')]
final class ConsumerDisableCommand extends Command
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

        $this->thumper->disableConsumer($name);

        $output->writeln("Consumer <comment>{$name}</comment> disabled.");

        return Command::SUCCESS;
    }
}
