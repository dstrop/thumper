# Thumper

Thumper is a simple RabbitMQ client for RoadRunner workers.  
It utilizes RoadRunner's process manager to manage RabbitMQ consumers.

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
    -e VERSION="v2024.1.3-thumper-$TAG" \
    -e TIME="$(date +%FT%T%z)" \
    -e GOOS=linux \
    -e GOARCH=amd64 \
    velox
```

### Using custom velox config

To use a custom Velox configuration, add this plugin to the `plugins` section of your `velox_rr.toml` file:

```toml
[plugins]
thumper = { ref = "v0.0.0", owner = "dstrop", repository = "thumper" }
```

Replace `v0.0.0` with the actual version you want to use. You can find the version by checking the latest tags in the `thumper` repository.

For more detailed information on customizing the build process, refer to the [RoadRunner customization documentation](https://docs.roadrunner.dev/docs/customization/build).

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
