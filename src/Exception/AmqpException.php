<?php

declare(strict_types=1);

namespace Thumper\Exception;

/**
 * The broker refused the operation. getCode() returns the AMQP reply code.
 */
class AmqpException extends ThumperException {}
