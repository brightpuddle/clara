.PHONY: proto build build-server build-agent build-client clean docker-up docker-down

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

docker-up:
	docker compose -f docker/docker-compose.yml up -d

docker-down:
	docker compose -f docker/docker-compose.yml down

ollama-pull:
	docker compose -f docker/docker-compose.yml exec ollama ollama pull nomic-embed-text
