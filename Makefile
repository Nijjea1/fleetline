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
