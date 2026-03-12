.PHONY: build test vet lint proto fmt clean

GOLINES_FLAGS := -m 100 --base-formatter goimports

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

## proto: regenerate Go and Swift protobuf bindings
proto:
	protoc \
		-I proto \
		--go_out=internal/bridge/gen \
		--go_opt=module=github.com/brightpuddle/clara/internal/bridge/gen \
		--go-grpc_out=internal/bridge/gen \
		--go-grpc_opt=module=github.com/brightpuddle/clara/internal/bridge/gen \
		proto/bridge.proto
	protoc \
		-I proto \
		--swift_out=swift/Sources/Proto \
		--swift_opt=Visibility=Public \
		--grpc-swift_out=Visibility=Public:swift/Sources/Proto \
		--plugin=protoc-gen-grpc-swift=/opt/homebrew/bin/protoc-gen-grpc-swift-2 \
		proto/bridge.proto

## bridge: build the Swift ClaraBridge binary
bridge:
	cd swift && swift build -c release
	cp swift/.build/release/ClaraBridge ./ClaraBridge

## clean: remove build artifacts
clean:
	rm -f clara ClaraBridge
	rm -rf swift/.build
