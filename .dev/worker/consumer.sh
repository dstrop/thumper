#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

cd /plugin

reflex --decoration=none --regex=\\.go$ -s -- bash -euo pipefail -c '
    cd /plugin/.dev/rr
    go build -o /tmp/rr /plugin/.dev/rr/main.go
    cd /plugin/.dev/worker
    /tmp/rr
'
