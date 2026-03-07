.DEFAULT_GOAL := help

BINARY_DIR    := bin
SERVER_BIN    := $(BINARY_DIR)/clara-server
AGENT_BIN     := $(BINARY_DIR)/clara-agent
TUI_BIN       := $(BINARY_DIR)/clara-tui

GO            := go
BUF           := buf
AIR           := air
GOREMAN       := goreman

.PHONY: help
help: ## Display available make targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' | \
		sort

.PHONY: setup
setup: ## Install required toolchain (buf, protoc-gen-go, protoc-gen-go-grpc, air, goreman)
	@echo "Installing protoc-gen-go..."
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@echo "Installing protoc-gen-go-grpc..."
	$(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "Installing air..."
	$(GO) install github.com/air-verse/air@latest
	@echo "Installing goreman..."
	$(GO) install github.com/mattn/goreman@latest
	@echo "Setup complete."

.PHONY: proto
proto: ## Generate gRPC/protobuf Go stubs from .proto files
	$(BUF) generate

.PHONY: build
build: proto ## Compile all Go binaries
	@mkdir -p $(BINARY_DIR)
	$(GO) build -o $(SERVER_BIN) ./cmd/server
	$(GO) build -o $(AGENT_BIN)  ./cmd/agent
	$(GO) build -o $(TUI_BIN)    ./cmd/tui
	@echo "Built: $(SERVER_BIN), $(AGENT_BIN), $(TUI_BIN)"

.PHONY: dev
dev: ## Run server, agent, and native worker together via goreman
	$(GOREMAN) start

.PHONY: dev-server
dev-server: ## Run server with air hot-reload (--debug enabled)
	$(AIR) -c .air.server.toml

.PHONY: dev-agent
dev-agent: ## Run agent with air hot-reload (--debug enabled)
	$(AIR) -c .air.agent.toml

.PHONY: dev-tui
dev-tui: ## Run TUI directly (no hot-reload needed for interactive TUI)
	$(GO) run ./cmd/tui

.PHONY: clean
clean: ## Remove compiled binaries
	rm -rf $(BINARY_DIR)

.PHONY: tidy
tidy: ## Run go mod tidy
	$(GO) mod tidy

.PHONY: swift-build
swift-build: ## Build the Swift native worker
	cd native && swift build -c release

.PHONY: swift-build-debug
swift-build-debug: ## Build the Swift native worker (debug)
	cd native && swift build
