<?php

declare(strict_types=1);

namespace Thumper\Command;

use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Helper\Table;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Thumper\ThumperInterface;

#[AsCommand(name: 'thumper:status', description: 'Show runtime status of Thumper consumers and workers')]
final class StatusCommand extends Command
{
    public function __construct(private readonly ThumperInterface $thumper)
    {
        parent::__construct();
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $status = $this->thumper->status();

        $output->writeln('<info>Consumers</info>');
        $consumers = new Table($output);
        $consumers->setHeaders(['Consumer', 'Queue', 'Enabled', 'Running', 'Concurrency', 'Prefetch']);
        foreach ($status['consumers'] as $name => $consumer) {
            $consumers->addRow([
                $name,
                $consumer['queue'],
                $consumer['enabled'] ? '<info>yes</info>' : '<comment>no</comment>',
                $consumer['running'] ? '<info>yes</info>' : '<comment>no</comment>',
                $consumer['concurrency'],
                $consumer['prefetch'],
            ]);
        }
        $consumers->render();

        $output->writeln('');

        $output->writeln('<info>Workers</info>');
        $workers = new Table($output);
        $workers->setHeaders(['PID', 'Status', 'Execs', 'Memory', 'CPU%']);
        foreach ($status['workers'] as $worker) {
            $workers->addRow([
                $worker['pid'],
                $worker['statusStr'],
                $worker['numExecs'],
                $this->formatMemory((int) $worker['memoryUsage']),
                \number_format((float) $worker['CPUPercent'], 1) . '%',
            ]);
        }
        $workers->render();

        return Command::SUCCESS;
    }

    private function formatMemory(int $bytes): string
    {
        if ($bytes >= 1024 * 1024) {
            return \number_format($bytes / (1024 * 1024), 1) . ' MB';
        }
        return \number_format($bytes / 1024, 1) . ' KB';
    }
}
