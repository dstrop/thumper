<?php

declare(strict_types=1);

namespace Thumper\Exception;

/**
 * AMQP 405: the resource is locked, e.g. an exclusive queue owned by another connection.
 */
class ResourceLockedException extends AmqpException {}
