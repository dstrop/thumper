<?php

declare(strict_types=1);

namespace Thumper\Exception;

/**
 * AMQP 403: the user has no permission for the resource.
 */
class AccessRefusedException extends AmqpException {}
