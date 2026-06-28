set shell := ["bash", "-cu"]

binary := "tldw"
justfile_dir := justfile_directory()
tunnel_client := env_var_or_default("TLDW_TUNNEL_CLIENT", "./bin/tunnel-client")
tunnel_profile := env_var_or_default("TLDW_TUNNEL_PROFILE", "tldw")
tunnel_id := env_var_or_default("TLDW_TUNNEL_ID", "")
mcp_command := env_var_or_default("TLDW_MCP_COMMAND", "tldw mcp")
launchd_label := env_var_or_default("TLDW_TUNNEL_LAUNCHD_LABEL", "dev.rtzll.tldw.tunnel")
launchd_keychain_service := env_var_or_default("TLDW_TUNNEL_KEYCHAIN_SERVICE", "tldw-tunnel-control-plane-api-key")

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

tunnel-launchd-install:
    /bin/bash "{{justfile_dir}}/scripts/tunnel-launchd" install \
        "{{launchd_label}}" \
        "{{launchd_keychain_service}}" \
        "{{tunnel_client}}" \
        "{{tunnel_profile}}" \
        "{{justfile_dir}}"

tunnel-launchd-uninstall:
    /bin/bash "{{justfile_dir}}/scripts/tunnel-launchd" uninstall "{{launchd_label}}"

tunnel-launchd-status:
    /bin/bash "{{justfile_dir}}/scripts/tunnel-launchd" status "{{launchd_label}}"

tunnel-launchd-logs:
    /bin/bash "{{justfile_dir}}/scripts/tunnel-launchd" logs

help:
    @just --list
