# LiteLLM Go proxy Makefile

GO_PROXY_PACKAGE := ./cmd/litellm-proxy
GO_PROXY_BINARY  := bin/litellm-proxy

.PHONY: help build test test-race run local-dev

help:
	@echo "Available commands:"
	@echo "  make build       - Build the Go proxy binary"
	@echo "  make test        - Run Go tests"
	@echo "  make test-race   - Run Go tests with the race detector"
	@echo "  make run         - Run the proxy with config.yaml"
	@echo "  make local-dev   - Run the proxy in local-dev mode"

build:
	@mkdir -p $$(dirname "$(GO_PROXY_BINARY)")
	go build -o "$(GO_PROXY_BINARY)" $(GO_PROXY_PACKAGE)

test:
	go test ./cmd/litellm-proxy ./internal/... ./pkg/... ./litellm

test-race:
	go test -race ./cmd/litellm-proxy ./internal/... ./pkg/... ./litellm

run:
	go run $(GO_PROXY_PACKAGE) --config config.yaml

local-dev:
	go run $(GO_PROXY_PACKAGE) --config config.yaml --local-dev
