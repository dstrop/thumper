.PHONY: help test test-cover test-cover-html

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

build: ## build dev rr binary for amd64
	docker compose run --rm -e GOARCH=amd64 velox
	docker compose run --rm --entrypoint chown velox "$(shell id -u):$(shell id -g)" rr
	chmod a+x rr

build-arm: ## build dev rr binary for arm64
	docker compose run --rm -e GOARCH=arm64 velox
	docker compose run --rm --entrypoint chown velox "$(shell id -u):$(shell id -g)" rr
	chmod a+x rr

composer: ## install composer dependencies for example worker and plugin
	docker compose run --no-deps --rm -u "$(shell id -u):$(shell id -g)" -w /plugin consumer composer install
	docker compose run --no-deps --rm -u "$(shell id -u):$(shell id -g)" consumer composer install

test: ## run go tests
	docker compose run --no-deps --rm -w /plugin consumer go test ./...

# COVER_PKGS lists only packages with tests
COVER_PKGS = $$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)

test-cover: ## run go tests with coverage summary
	docker compose run --no-deps --rm -w /plugin consumer sh -c "go test -coverprofile=coverage.out $(COVER_PKGS) && go tool cover -func=coverage.out"

test-cover-html: test-cover ## open the html coverage report in a browser
	docker compose run --no-deps --rm -w /plugin consumer go tool cover -html=coverage.out -o coverage.html
	xdg-open coverage.html

mod-tidy: ## run go mod tidy
	docker compose run --no-deps --rm -w /plugin consumer go mod tidy
	docker compose run --no-deps --rm consumer go mod tidy

up: up-consumer up-producer ## start example consumer and producer

up-consumer: ## start example consumer
	docker compose up --wait -d consumer
	docker compose logs -f consumer

up-producer: ## run example producer
	docker compose exec consumer php producer.php

exec-cli: ## execute cli command
	docker compose exec consumer php cli.php

lint: lint-go lint-php ## run linters

lint-go: ## run golangci-lint
	docker compose run --rm --no-deps -w /plugin --entrypoint golangci-lint consumer run ./...

lint-php: phpcs phpstan ## run php-cs-fixer and phpstan

phpcs: ## run php-cs-fixer check
	docker compose run --no-deps -w /plugin --rm consumer composer phpcs

phpcs-fix: ## run php-cs-fixer fix
	docker compose run --no-deps -w /plugin --rm  -u "$(shell id -u):$(shell id -g)" consumer composer phpcs-fix

phpstan: ## run phpstan
	docker compose run --no-deps -w /plugin --rm consumer composer phpstan
