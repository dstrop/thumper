.PHONY: help

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

build: ## build dev rr binary for amd64
	docker compose run --rm -e TIME="$(shell date +%FT%T%z)" -e GOARCH=amd64 velox
	docker compose run --rm --entrypoint chown velox "$(shell id -u):$(shell id -g)" rr
	chmod a+x rr

build-arm: ## build dev rr binary for arm64
	docker compose run --rm -e TIME="$(shell date +%FT%T%z)" -e GOARCH=arm64 velox
	docker compose run --rm --entrypoint chown velox "$(shell id -u):$(shell id -g)" rr
	chmod a+x rr

composer: ## install composer dependencies for example worker and plugin
	docker compose run --no-deps --rm -u "$(shell id -u):$(shell id -g)" -w /plugin consumer composer install
	docker compose run --no-deps --rm -u "$(shell id -u):$(shell id -g)" consumer composer install

mod-tidy: ## run go mod tidy
	docker compose run --no-deps --rm -w /plugin consumer go mod tidy
	docker compose run --no-deps --rm consumer go mod tidy

up: up-consumer up-producer ## start example consumer and producer

up-consumer: ## start example consumer
	docker compose up --wait -d consumer
	docker compose logs -f consumer

up-producer: ## run example producer
	docker compose exec consumer php producer.php

lint: lint-go lint-php ## run linters

lint-go: ## run golangci-lint
	docker compose run --rm --no-deps -w /plugin --entrypoint golangci-lint consumer run ./...

lint-php: phpcs phpstan ## run phpcs and phpstan

phpcs: ## run phpcs
	docker compose run --no-deps -w /plugin --rm consumer composer phpcs

phpstan: ## run phpstan
	docker compose run --no-deps -w /plugin --rm consumer composer phpstan
