<?php

declare(strict_types=1);

namespace Thumper\Exception;

/**
 * AMQP 404: the exchange or queue does not exist.
 */
class NotFoundException extends AmqpException {}
