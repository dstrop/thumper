<?php

declare(strict_types=1);

namespace Thumper\Exception;

/**
 * The broker did not accept a published message (publisher confirm was negative).
 */
class PublishNackedException extends ThumperException {}
