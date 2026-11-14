<?php

declare(strict_types=1);

namespace Thumper\Exception;

/**
 * The operation did not finish in time. A published message may or may not have reached the broker.
 */
class TimeoutException extends ThumperException {}
