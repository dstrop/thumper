# TODO

## Post-v1

- **deliveryTag-based ack control.** Today the single result byte is the only
  control channel and `deliveryTag` in the payload is informational. A richer
  protocol could let the PHP worker ack mid-processing (ack early, keep
  working), ack multiple deliveries at once, or defer settlement across
  messages. Needs a way to route the ack back to the channel the delivery
  arrived on, so it is interesting but probably not for the first version.
- **Poison-message guard.** A message that always fails with
  `requeue_on_fail: true` loops forever. Consider a redelivery cap using
  `Delivery.Redelivered` / `x-death` headers, with a dead-letter or drop
  policy once exceeded.
- **Pause vs disable.** Disable cancels the AMQP consumer entirely. A
  lighter "pause" (keep the consumer registered, stop dispatching — i.e.
  `Resize(0)` meaning blocked rather than unlimited) could be useful, but the
  semantics of concurrency 0 = unlimited conflict with it; would need a
  separate flag.

## Open questions / smaller items

- Reconnect backoff for the consume loop: hardcoded exponential w/ cap, or
  config?
- Per-consumer `exec_timeout` override (global default in `Config` now).
- Persist runtime `Enable`/`Disable`/`SetConcurrency` changes across a config
  reload — currently they live only in the in-memory config map.
- Metrics/events: processed/acked/nacked counters and in-flight gauge per
  queue (Status already exposes in-flight).
- Document that handlers must be idempotent: if the connection drops while a
  message is being processed, the final ack fails (logged) and the broker
  redelivers — standard AMQP behavior.
