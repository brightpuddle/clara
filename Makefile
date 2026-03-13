.PHONY: build test vet lint proto fmt bridge clean

GOLINES_FLAGS := -m 100 --base-formatter goimports
BRIDGE_APP_DIR := /usr/local/libexec/ClaraBridge.app
BRIDGE_APP_EXE := $(BRIDGE_APP_DIR)/Contents/MacOS/ClaraBridge
BRIDGE_WRAPPER := /usr/local/bin/ClaraBridge

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

## clean: remove build artifacts
clean:
	rm -f clara ClaraBridge
	rm -rf swift/.build
