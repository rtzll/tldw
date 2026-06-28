set shell := ["bash", "-cu"]

binary := "tldw"
tunnel_client := env_var_or_default("TLDW_TUNNEL_CLIENT", "./bin/tunnel-client")
tunnel_profile := env_var_or_default("TLDW_TUNNEL_PROFILE", "tldw")
tunnel_id := env_var_or_default("TLDW_TUNNEL_ID", "")
mcp_command := env_var_or_default("TLDW_MCP_COMMAND", "tldw mcp")

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

update:
    go get -u ./...
    go mod tidy

fmt:
    go fmt ./...

check: fmt lint test

tunnel-init:
    #!/usr/bin/env bash
    set -euo pipefail
    test -n "{{tunnel_id}}" || { echo "Set TLDW_TUNNEL_ID=tunnel_..."; exit 1; }
    test -x "{{tunnel_client}}" || { echo "Install tunnel-client at {{tunnel_client}} or set TLDW_TUNNEL_CLIENT"; exit 1; }
    "{{tunnel_client}}" init --sample sample_mcp_stdio_local --profile "{{tunnel_profile}}" --tunnel-id "{{tunnel_id}}" --mcp-command "{{mcp_command}}"

tunnel-doctor:
    test -x "{{tunnel_client}}" || { echo "Install tunnel-client at {{tunnel_client}} or set TLDW_TUNNEL_CLIENT"; exit 1; }
    "{{tunnel_client}}" doctor --profile "{{tunnel_profile}}" --explain

tunnel-run:
    test -x "{{tunnel_client}}" || { echo "Install tunnel-client at {{tunnel_client}} or set TLDW_TUNNEL_CLIENT"; exit 1; }
    "{{tunnel_client}}" run --profile "{{tunnel_profile}}"

help:
    @just --list
