BINARY_NAME  := copera
VERSION      := $(shell cat VERSION 2>/dev/null || echo "dev")
BUILD_TIME   := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
MODULE       := github.com/copera/copera-cli
LDFLAGS      := -ldflags "-s -w \
	-X $(MODULE)/internal/build.Version=$(VERSION) \
	-X $(MODULE)/internal/build.Time=$(BUILD_TIME)"
DIST_DIR     := dist

.PHONY: build build-all clean run test test-integration deps fmt lint install version

## build: compile for the current platform
build:
	@mkdir -p $(DIST_DIR)
	go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME) ./cmd/copera/

## build-all: cross-compile for all target platforms
build-all:
	@mkdir -p $(DIST_DIR)
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64   ./cmd/copera/
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64   ./cmd/copera/
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64  ./cmd/copera/
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64  ./cmd/copera/
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/copera/

## clean: remove build artifacts
clean:
	rm -rf $(DIST_DIR)

## run: run the CLI (pass ARGS="..." to forward arguments)
run:
	go run ./cmd/copera/ $(ARGS)

## test: run unit tests (no network, no real tokens)
test:
	go test -race -count=1 ./...

## test-integration: run integration tests (requires COPERA_CLI_AUTH_TOKEN)
test-integration:
	COPERA_INTEGRATION_TEST=1 go test -race -count=1 -run Integration ./...

## deps: download and tidy dependencies
deps:
	go mod download
	go mod tidy

## fmt: format all Go source files
fmt:
	gofmt -w .

## lint: run golangci-lint
lint:
	golangci-lint run

## install: build and copy binary to /usr/local/bin
install: build
	cp $(DIST_DIR)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

## version: print the current version
version:
	@cat VERSION 2>/dev/null || echo "dev"

# help: print this help
help:
	@grep -E '^## ' Makefile | sed 's/## //'
