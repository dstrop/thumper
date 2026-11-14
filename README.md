# Thumper

Thumper is a simple RabbitMQ client for RoadRunner workers.  
It uses RoadRunner's process manager to manage RabbitMQ consumers.

Main goals of this plugin are:
- allow single worker to consume multiple queues
- replace php amqp libraries

## Project status

This project is in early development stage.  
It is not recommended to use it in production.

## Build

### Using prepared [velox_rr.toml](velox_rr.toml)
Copy the [.env.example](.env.example) file to `.env` and fill in your github token.

```bash
docker compose run --rm \
    -e RT_TOKEN=read_only_github_token \
    -e GOARCH=amd64 \
    velox
```

### Using custom velox config

You can use the [RoadRunner Velox Builder](https://build.roadrunner.dev/) to generate a base `velox_rr.toml` or `Dockerfile` with your other desired plugins.

Thumper is not available in the builder, so you will need to manually add it to the `plugins` section of your config:

```toml
[plugins.thumper]
module_name = "github.com/dstrop/thumper"
tag = "v0.0.0"
```

Replace `v0.0.0` with the actual version you want to use. You can find the version by checking the latest tags in the `thumper` repository.

For more build options, see the [RoadRunner customization documentation](https://docs.roadrunner.dev/docs/customization/build).

## Error handling

`publish`, `exchangeDeclare`, `queueDeclare` and `bindQueue` throw RabbitMQ errors as typed exceptions.
All of them are in the `Thumper\Exception` namespace and extend `ThumperException`.

| Exception | Meaning |
|---|---|
| `AmqpException` | The broker refused the operation. `getCode()` is the AMQP reply code. |
| `NotFoundException` | AMQP 404, the exchange or queue does not exist. |
| `AccessRefusedException` | AMQP 403, no permission for the resource. |
| `ResourceLockedException` | AMQP 405, the resource is locked. |
| `PreconditionFailedException` | AMQP 406, the resource exists with different properties. |
| `PublishNackedException` | The broker did not accept a published message. |
| `TimeoutException` | The operation did not finish in time. |
| `ConnectionException` | There is no usable connection to the broker. |

The four AMQP code exceptions extend `AmqpException`.
`ThumperException` extends goridge's `ServiceException`, which is still what other plugin errors are thrown as.

```php
try {
    $thumper->publish('orders', 'order.created', 'application/json', $body);
} catch (TimeoutException | ConnectionException $e) {
    // the broker is unreachable or slow, try again later
} catch (ThumperException $e) {
    // the broker refused the message
}
```

## Development

The project is setup to have the dev env in docker.
And all normal commands are wrapped in `make` commands.

### Setup

- Install the php dependencies for the plugin and example worker.
  ```bash
  make composer
  ```

- Start example consumer.  
  Consumer runs under reflex and will restart on code changes.
  ```bash
  make up-consumer
  ```

- Run the example producer.
  ```bash
  make up-producer
  ```

- Review the make commands.
  ```bash
  make help
  ```
