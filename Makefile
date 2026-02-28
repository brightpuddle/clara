.PHONY: proto build build-server build-agent build-client clean \
        docker-up docker-down setup-ollama install-config \
        dev-server dev-agent help

BINARY_SERVER = clara-server
BINARY_AGENT  = clara-agent
BINARY_CLIENT = clara

# Print this help message (all targets annotated with ##).
help:
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage: make \033[36m<target>\033[0m\n\nTargets:\n"} \
	      /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""

proto: ## Regenerate protobuf / gRPC code via buf
	buf generate

build: build-server build-agent build-client ## Build all three binaries

build-server: ## Build clara-server
	go build -o $(BINARY_SERVER) ./server

build-agent: ## Build clara-agent
	go build -o $(BINARY_AGENT) ./agent

build-client: ## Build clara (TUI client)
	go build -o $(BINARY_CLIENT) ./client

clean: ## Remove compiled binaries
	rm -f $(BINARY_SERVER) $(BINARY_AGENT) $(BINARY_CLIENT)

docker-up: ## Start infrastructure (postgres+pgvector, Temporal) in Docker
	docker compose -f docker/docker-compose.yml up -d

docker-down: ## Stop and remove infrastructure containers
	docker compose -f docker/docker-compose.yml down

# One-time setup: Ollama must run natively on Apple Silicon to use the Metal GPU.
# Docker/Podman runs in a Linux VM and has no access to Metal — 5-10× slower.
setup-ollama: ## (One-time) Install Ollama via Homebrew and pull embedding model
	@which ollama > /dev/null 2>&1 || brew install ollama
	brew services start ollama
	ollama pull nomic-embed-text
	@echo "Ollama running at http://localhost:11434"

install-config: ## Copy example configs to ~/.config/clara/ (won't overwrite existing)
	@mkdir -p "$${XDG_CONFIG_HOME:-$$HOME/.config}/clara"
	@for f in config/*.yaml.example; do \
		dest="$${XDG_CONFIG_HOME:-$$HOME/.config}/clara/$$(basename $$f .example)"; \
		if [ -f "$$dest" ]; then \
			echo "skip (exists): $$dest"; \
		else \
			cp "$$f" "$$dest"; \
			echo "installed: $$dest"; \
		fi; \
	done

dev-server: ## Live-reload server (air) — rebuilds on any Go source change under ./server
	air -c .air.server.toml

dev-agent: ## Live-reload agent (air) — rebuilds on any Go source change under ./agent
	air -c .air.agent.toml
