BINARY_NAME=notify-klipper
VERSION ?= 0.1.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -w -s -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)

IMAGE_NAME ?= notify-klipper:$(VERSION)
CONTAINER_CLI ?= $(shell which podman 2>/dev/null || which docker 2>/dev/null || echo "docker")

.PHONY: all build build-all test clean container run-test

all: test build

build:
	@echo "Building $(BINARY_NAME) for host..."
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/notify-klipper

build-all:
	@echo "Cross-compiling $(BINARY_NAME) for linux/amd64 and linux/arm64..."
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-amd64 ./cmd/notify-klipper
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-arm64 ./cmd/notify-klipper
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY_NAME)-darwin-arm64 ./cmd/notify-klipper
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY_NAME)-darwin-amd64 ./cmd/notify-klipper
	@echo "Binaries created in dist/:"
	@ls -la dist/

test:
	@echo "Running tests..."
	go test -v -race ./...

run-test: build
	@echo "Running test mode against configured credentials..."
	./bin/$(BINARY_NAME) --test

container:
	@echo "Building container image using $(CONTAINER_CLI)..."
	$(CONTAINER_CLI) build -t $(IMAGE_NAME) -f Containerfile .

clean:
	@echo "Cleaning artifacts..."
	rm -rf bin dist
