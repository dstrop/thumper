<?php

declare(strict_types=1);

$finder = PhpCsFixer\Finder::create()
    ->in(__DIR__ . '/src')
    ->in(__DIR__ . '/.dev');

return (new PhpCsFixer\Config())
    ->setRules([
        '@PER-CS3.0' => true,
    ])
    ->setFinder($finder);
