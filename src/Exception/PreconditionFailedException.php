<?php

declare(strict_types=1);

namespace Thumper\Exception;

/**
 * AMQP 406: the resource exists with different properties, or the arguments are invalid.
 */
class PreconditionFailedException extends AmqpException {}
