.PHONY: proto build build-server build-agent build-client clean docker-up docker-down setup

BINARY_SERVER = clara-server
BINARY_AGENT  = clara-agent
BINARY_CLIENT = clara

proto:
	buf generate

build: build-server build-agent build-client

build-server:
	go build -o $(BINARY_SERVER) ./server

build-agent:
	go build -o $(BINARY_AGENT) ./agent

build-client:
	go build -o $(BINARY_CLIENT) ./client

clean:
	rm -f $(BINARY_SERVER) $(BINARY_AGENT) $(BINARY_CLIENT)

# Start infrastructure (postgres + temporal). Ollama runs natively — see setup target.
docker-up:
	docker compose -f docker/docker-compose.yml up -d

docker-down:
	docker compose -f docker/docker-compose.yml down

# One-time setup for the M4 Mini: install and configure native Ollama via Homebrew.
# Ollama must run natively to use Apple Silicon GPU (Metal). Docker blocks GPU access.
setup-ollama:
	@which ollama > /dev/null 2>&1 || brew install ollama
	brew services start ollama
	ollama pull nomic-embed-text
	@echo "Ollama running at http://localhost:11434"
