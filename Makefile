.PHONY: build test vet lint proto fmt bridge clean install uninstall

GOLINES_FLAGS := -m 100 --base-formatter goimports
BRIDGE_APP_DIR := /usr/local/libexec/ClaraBridge.app
BRIDGE_APP_EXE := $(BRIDGE_APP_DIR)/Contents/MacOS/ClaraBridge
BRIDGE_WRAPPER := /usr/local/bin/ClaraBridge
INSTALL_BIN := /usr/local/bin/clara
LAUNCH_AGENT_DIR := $(HOME)/Library/LaunchAgents
LAUNCH_AGENT_FILE := com.brightpuddle.clara.agent.plist
LAUNCH_AGENT_PLIST := $(LAUNCH_AGENT_DIR)/$(LAUNCH_AGENT_FILE)

## build: compile the unified clara binary
build:
	go build -o clara ./cmd/clara

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
bridge:
	cd swift && swift build -c release
	rm -rf "$(BRIDGE_APP_DIR)"
	mkdir -p "$(BRIDGE_APP_DIR)/Contents/MacOS"
	cp swift/.build/release/ClaraBridge "$(BRIDGE_APP_EXE)"
	cp swift/Sources/ClaraBridge/Info.plist "$(BRIDGE_APP_DIR)/Contents/Info.plist"
	codesign --force --deep --sign - "$(BRIDGE_APP_DIR)"
	cp "$(BRIDGE_APP_EXE)" ./ClaraBridge
	printf '#!/bin/sh\nexec \"%s\" \"$@\"\n' "$(BRIDGE_APP_EXE)" > "$(BRIDGE_WRAPPER)"
	chmod +x "$(BRIDGE_WRAPPER)"

## install: install clara and restart or start the LaunchAgent
install: build
	install -d "$(LAUNCH_AGENT_DIR)"
	install -m 755 clara "$(INSTALL_BIN)"
	install -m 644 "$(LAUNCH_AGENT_FILE)" "$(LAUNCH_AGENT_PLIST)"
	"$(INSTALL_BIN)" agent stop >/dev/null 2>&1 || true
	"$(INSTALL_BIN)" agent start

## uninstall: stop Clara and remove the installed binary and LaunchAgent
uninstall:
	@if [ -x "$(INSTALL_BIN)" ]; then "$(INSTALL_BIN)" agent stop >/dev/null 2>&1 || true; fi
	rm -f "$(LAUNCH_AGENT_PLIST)"
	rm -f "$(INSTALL_BIN)"

## clean: remove build artifacts
clean:
	rm -f clara ClaraBridge
	rm -rf swift/.build
