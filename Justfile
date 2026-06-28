set shell := ["bash", "-cu"]

binary := "tldw"

default: help

build:
    go build -o {{binary}} -v .

test:
    go test ./...

lint:
    golangci-lint run

install-tools:
    @echo "Installing golangci-lint..."
    @which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

tidy:
    go mod tidy

fmt:
    go fmt ./...

check: fmt lint test

help:
    @just --list
