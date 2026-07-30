# Configuration

Thumper is configured under the `thumper` key in your RoadRunner `.rr.yaml`.

## Full example

```yaml
thumper:
  disable_consumers: false

  pool:
    num_workers: 4
    supervisor:
      ttl: 2h
      idle_ttl: 30m
      exec_ttl: 10m
      max_worker_memory: 800

  amqp:
    addr: amqp://user:password@rabbitmq:5672

    exchange:
      events:
        kind: direct
        durable: true

    queue:
      orders:
        durable: true
        args:
          x-message-ttl: 86400000
        bindings:
          events:                   # exchange name (or use explicit `exchange:` field)
            key: routing.key.orders

      payments:
        durable: true
        bindings:
          events:
            key: routing.key.payments

  consumers:
    orders:                         # queue name inferred from key
      concurrency: 5
      prefetch: 10
      requeue_on_fail: true

    payments:
      concurrency: 3
      prefetch: 5
```

## Reference

### `amqp`

| Field | Type | Description |
|-------|------|-------------|
| `addr` | string | AMQP connection URL. |
| `exchange` | map[string][exchange](#exchange) | Exchange declarations. |
| `queue` | map[string][queue](#queue) | Queue declarations. |

### `exchange`

Map key is used as `name` if `name` is not set explicitly.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | *(map key)* | Exchange name. Supports env expansion. |
| `kind` | string | | Exchange type: `direct`, `fanout`, `topic`, `headers`. Supports env expansion. |
| `durable` | bool | `false` | Survives broker restart. |
| `autoDelete` | bool | `false` | Deleted when last consumer unsubscribes. |
| `internal` | bool | `false` | Not accessible directly by publishers. |
| `noWait` | bool | `false` | Do not wait for server confirmation. |
| `args` | map | | Additional exchange arguments. String values support env expansion. |

### `queue`

Map key is used as `name` if `name` is not set explicitly.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | *(map key)* | Queue name. Supports env expansion. |
| `durable` | bool | `false` | Survives broker restart. |
| `autoDelete` | bool | `false` | Deleted when last consumer unsubscribes. |
| `exclusive` | bool | `false` | Only accessible by the declaring connection. |
| `noWait` | bool | `false` | Do not wait for server confirmation. |
| `args` | map[string]any | | Additional queue arguments (e.g. `x-message-ttl`). String values support env expansion. |
| `bindings` | map[string][binding](#bindings) | | Bindings to exchanges. |

### `bindings`

Map key is used as `exchange` if `exchange` is not set explicitly. `queue` is implicit from the parent.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `exchange` | string | *(map key)* | Exchange to bind to. Supports env expansion. |
| `key` | string | | Routing key. Supports env expansion. |
| `noWait` | bool | `false` | Do not wait for server confirmation. |
| `args` | map[string]any | | Additional binding arguments. String values support env expansion. |

### `consumers`

Map key is used as `queue` if `queue` is not set explicitly.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `queue` | string | *(map key)* | Queue to consume from. Supports env expansion. |
| `enabled` | bool | `true` | Set to `false` to declare the consumer without starting it. |
| `concurrency` | int | | Number of concurrent message handlers. |
| `prefetch` | int | `1` | Number of messages to prefetch per consumer. |
| `durable` | bool | `false` | Consumer survives connection restart. |
| `requeue_on_fail` | bool | `true` | Requeue message when worker returns an error. |
| `consumer_id` | string | | Consumer tag. Supports env expansion. |

### `pool`

Worker pool configuration. See [RoadRunner pool documentation](https://docs.roadrunner.dev/docs/php-worker/pool) for all available options.

### `disable_consumers`

Runs thumper without consumers: no pool is started and no PHP workers are spawned. Exchanges, queues and bindings are still declared and RPC publishing stays available. Implied when no `consumers` are configured.
