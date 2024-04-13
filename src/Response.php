<?php declare(strict_types=1);

namespace Thumper;

enum Response: int
{
    case Ack = 0;
    case Nack = 1;
    case Reject = 2;
}
