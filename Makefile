.PHONY: build test lint install-tools help

BINARY_NAME=tldw

GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
GOMOD=$(GOCMD) mod

build:
	$(GOBUILD) -o $(BINARY_NAME) -v .

test:
	$(GOTEST) ./...
lint:
	golangci-lint run

install-tools:
	@echo "Installing golangci-lint..."
	@which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

tidy:
	$(GOMOD) tidy

fmt:
	$(GOCMD) fmt ./...

check: fmt lint test

help:
	@echo "Available targets:"
	@echo "  build         - Build the application"
	@echo "  test          - Run tests"
	@echo "  lint          - Run linter"
	@echo "  install-tools - Install development tools"
	@echo "  tidy          - Tidy dependencies"
	@echo "  fmt           - Format code"
	@echo "  check         - Run all checks (fmt, lint, test)"
	@echo "  help          - Show this help"

all: build