.PHONY: build test vet lint proto fmt bridge clean install install-clara uninstall sign-cert setup check-deps

GOLINES_FLAGS := -m 100 --base-formatter goimports
BRIDGE_APP_DIR := /usr/local/libexec/ClaraBridge.app
BRIDGE_APP_EXE := $(BRIDGE_APP_DIR)/Contents/MacOS/ClaraBridge
BRIDGE_WRAPPER := /usr/local/bin/ClaraBridge
INSTALL_BIN := /usr/local/bin/clara
LAUNCH_AGENT_DIR := $(HOME)/Library/LaunchAgents
LAUNCH_AGENT_FILE := com.brightpuddle.clara.agent.plist
LAUNCH_AGENT_PLIST := $(LAUNCH_AGENT_DIR)/$(LAUNCH_AGENT_FILE)

BRIDGE_SOURCES := $(shell find swift/Sources swift/Package.swift -type f 2>/dev/null)

# Use a persistent local certificate when available so that TCC grants (Full Disk
# Access, Reminders, Calendar, etc.) survive rebuilds.  Run `make sign-cert` once
# to create it; subsequent builds pick it up automatically.
CERT_NAME     := Clara Development
SIGN_IDENTITY := $(shell security find-identity -v -p codesigning 2>/dev/null \
                   | grep -q '"$(CERT_NAME)"' \
                   && echo '$(CERT_NAME)' || echo -)

## setup: install development dependencies (Homebrew + Go tools)
setup:
	@echo "Installing Homebrew dependencies..."
	brew install protobuf swift-protobuf
	@echo "Installing Go protobuf plugins..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "Building Swift gRPC v1 generator from source (required for ClaraBridge)..."
	@if [ ! -f /usr/local/bin/protoc-gen-grpc-swift ]; then \
		rm -rf /tmp/grpc-swift-src; \
		git clone --depth 1 --branch 1.23.0 https://github.com/grpc/grpc-swift.git /tmp/grpc-swift-src; \
		cd /tmp/grpc-swift-src && swift build -c release --product protoc-gen-grpc-swift; \
		sudo cp /tmp/grpc-swift-src/.build/release/protoc-gen-grpc-swift /usr/local/bin/; \
		rm -rf /tmp/grpc-swift-src; \
	fi
	@echo "Setup complete."

## check-deps: verify all required protobuf tools are installed
check-deps:
	@command -v protoc >/dev/null 2>&1 || { echo "Error: protoc not found. Run 'make setup'."; exit 1; }
	@command -v protoc-gen-go >/dev/null 2>&1 || { echo "Error: protoc-gen-go not found. Run 'make setup'."; exit 1; }
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || { echo "Error: protoc-gen-go-grpc not found. Run 'make setup'."; exit 1; }
	@command -v protoc-gen-swift >/dev/null 2>&1 || { echo "Error: protoc-gen-swift not found. Run 'make setup'."; exit 1; }
	@if [ ! -f /usr/local/bin/protoc-gen-grpc-swift ]; then \
		echo "Error: /usr/local/bin/protoc-gen-grpc-swift (v1) not found. Run 'make setup'."; \
		exit 1; \
	fi

## build: compile the unified clara binary and all plugins
build: build-core build-integrations
	codesign --force --deep --sign "$(SIGN_IDENTITY)" bin/clara

## proto: generate protobuf bindings for Go and Swift
proto: check-deps
	mkdir -p pkg/contract/proto
	protoc --go_out=pkg/contract/proto --go_opt=module=github.com/brightpuddle/clara/pkg/contract/proto \
		--go-grpc_out=pkg/contract/proto --go-grpc_opt=module=github.com/brightpuddle/clara/pkg/contract/proto \
		proto/clara/plugin/v1/integration.proto
	# Swift generation
	mkdir -p swift/Sources/ClaraBridge/Generated
	protoc --swift_out=swift/Sources/ClaraBridge/Generated \
		--grpc-swift_out=swift/Sources/ClaraBridge/Generated \
		--proto_path=proto \
		proto/clara/plugin/v1/integration.proto

build-core:
	mkdir -p bin
	go build -ldflags="-extldflags '-Wl,-sectcreate,__TEXT,__info_plist,cmd/clara/Info.plist'" -o bin/clara ./cmd/clara

build-integrations:
	# Note: shell, fs, and db are now built-in (Phase 2) - no plugin binaries.
	mkdir -p bin/integrations
	for d in cmd/integrations/*; do \
		if [ -d "$$d" ]; then \
			name=$$(basename "$$d"); \
			go build -o bin/integrations/$$name ./$$d; \
		fi; \
	done

## test: run all Go tests
test:
	go test ./... -timeout 60s

## vet: run go vet
vet:
	go vet ./...

## lint: run staticcheck
lint:
	staticcheck ./...

## fmt: format all Go code with golines + goimports
fmt:
	golines $(GOLINES_FLAGS) -w ./...
	goimports -w ./...

## bridge: build the Swift ClaraBridge binary
bridge: build/ClaraBridge.app/Contents/MacOS/ClaraBridge

build/ClaraBridge.app/Contents/MacOS/ClaraBridge: $(BRIDGE_SOURCES)
	@SIGN_IDENTITY="$(SIGN_IDENTITY)" ./scripts/build_bridge.sh

## release: run goreleaser to build artifacts locally (without publishing)
release:
	goreleaser release --snapshot --clean

## release-check: check if goreleaser is installed
release-check:
	@command -v goreleaser >/dev/null 2>&1 || { echo >&2 "goreleaser is not installed. Visit https://goreleaser.com/install/"; exit 1; }

## install: install clara, plugins, and ClaraBridge, and restart or start the LaunchAgent
install: build $(BRIDGE_APP_EXE)
	install -m 755 bin/clara "$(INSTALL_BIN)"
	# Re-sign at destination to ensure the embedded Info.plist is valid
	codesign --force --deep --sign "$(SIGN_IDENTITY)" "$(INSTALL_BIN)"
	
	mkdir -p $(HOME)/.config/clara/integrations
	if [ -d bin/integrations ]; then cp bin/integrations/* $(HOME)/.config/clara/integrations/; fi

	install -d "$(LAUNCH_AGENT_DIR)"
	install -m 644 "$(LAUNCH_AGENT_FILE)" "$(LAUNCH_AGENT_PLIST)"
	"$(INSTALL_BIN)" agent stop >/dev/null 2>&1 || true
	"$(INSTALL_BIN)" agent start

## install-clara: build and install only the Go clara agent and plugins
install-clara: build
	install -m 755 bin/clara "$(INSTALL_BIN)"
	codesign --force --deep --sign "$(SIGN_IDENTITY)" "$(INSTALL_BIN)"

	mkdir -p $(HOME)/.config/clara/integrations
	if [ -d bin/integrations ]; then cp bin/integrations/* $(HOME)/.config/clara/integrations/; fi

	install -d "$(LAUNCH_AGENT_DIR)"
	install -m 644 "$(LAUNCH_AGENT_FILE)" "$(LAUNCH_AGENT_PLIST)"
	"$(INSTALL_BIN)" agent stop >/dev/null 2>&1 || true
	"$(INSTALL_BIN)" agent start

$(BRIDGE_APP_EXE): build/ClaraBridge.app/Contents/MacOS/ClaraBridge
	rm -rf "$(BRIDGE_APP_DIR)"
	cp -R build/ClaraBridge.app "$(BRIDGE_APP_DIR)"
	# Re-sign at destination to ensure the signature is valid for the new path
	codesign --force --deep --sign "$(SIGN_IDENTITY)" "$(BRIDGE_APP_DIR)"
	# Install the wrapper that points to the .app bundle
	printf '#!/bin/bash\nexec "$(BRIDGE_APP_EXE)" "$$@"\n' > build/ClaraBridge.wrapper
	install -m 755 build/ClaraBridge.wrapper "$(BRIDGE_WRAPPER)"


## sign-cert: create a persistent local code-signing certificate (run once per machine)
sign-cert:
	@./scripts/create_signing_cert.sh

## uninstall: stop Clara and remove the installed binary and LaunchAgent
uninstall:
	@if [ -x "$(INSTALL_BIN)" ]; then "$(INSTALL_BIN)" agent stop >/dev/null 2>&1 || true; fi
	rm -f "$(LAUNCH_AGENT_PLIST)"
	rm -f "$(INSTALL_BIN)"
	rm -f "$(BRIDGE_WRAPPER)"
	rm -rf "$(BRIDGE_APP_DIR)"

## clean: remove build artifacts
clean:
	rm -f clara ClaraBridge
	rm -rf swift/.build dist build
