# Fleetline developer commands. Run `make help` to list them.
COMPOSE := docker compose -f infra/docker-compose.yml

.PHONY: help up down logs ps

help: ## List commands
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

up: ## Start Redpanda, Redpanda Console and Redis
	$(COMPOSE) up -d --wait

down: ## Stop local infrastructure
	$(COMPOSE) down

logs: ## Tail infrastructure logs
	$(COMPOSE) logs -f

ps: ## Show running services
	$(COMPOSE) ps

# ---- Go core ----
FROM ?= 07:00
TO ?= 07:20
SPEED ?= 10

.PHONY: build test test-integration fleetd worldsim demo

build: ## Build fleetd and worldsim into core/bin
	cd core && go build -o bin/fleetd ./cmd/fleetd && go build -o bin/worldsim ./cmd/worldsim

test: ## Unit tests (Go + Python)
	cd core && go test ./...
	cd sim && uv run pytest -q
	cd ml && uv run pytest -q
	cd matcher && uv run pytest -q
	cd routing && uv run pytest -q

test-integration: up ## Integration tests against local Redpanda + Redis
	cd core && go test -tags integration ./...

fleetd: build ## Run the dispatch service (metrics on :9100)
	cd core && ./bin/fleetd

worldsim: build ## Replay FROM..TO at SPEED x (run fleetd first, in another terminal)
	cd core && ./bin/worldsim -from $(FROM) -to $(TO) -speed $(SPEED)

demo: ## Live stack end to end: infra + fleetd + worldsim (FROM, TO, SPEED)
	FROM=$(FROM) TO=$(TO) SPEED=$(SPEED) ./scripts/demo.sh
