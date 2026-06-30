set shell := ["bash", "-cu"]

binary := "tldw"
justfile_dir := justfile_directory()
tunnel_client := env_var_or_default("TLDW_TUNNEL_CLIENT", "./bin/tunnel-client")
tunnel_profile := env_var_or_default("TLDW_TUNNEL_PROFILE", "tldw")
tunnel_id := env_var_or_default("TLDW_TUNNEL_ID", "")
mcp_http_command := env_var_or_default("TLDW_MCP_HTTP_COMMAND", "tldw mcp --transport=http")
mcp_http_host := env_var_or_default("TLDW_MCP_HTTP_HOST", "127.0.0.1")
mcp_http_port := env_var_or_default("TLDW_MCP_HTTP_PORT", "8765")
mcp_server_url := env_var_or_default("TLDW_MCP_SERVER_URL", "http://" + mcp_http_host + ":" + mcp_http_port + "/mcp")
tunnel_health_addr := env_var_or_default("TLDW_TUNNEL_HEALTH_ADDR", "127.0.0.1:8080")
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
    "{{tunnel_client}}" init \
        --sample sample_mcp_with_dcr \
        --profile "{{tunnel_profile}}" \
        --tunnel-id "{{tunnel_id}}" \
        --mcp-server-url "{{mcp_server_url}}" \
        --health-listen-addr "{{tunnel_health_addr}}"

tunnel-doctor:
    test -x "{{tunnel_client}}" || { echo "Install tunnel-client at {{tunnel_client}} or set TLDW_TUNNEL_CLIENT"; exit 1; }
    "{{tunnel_client}}" doctor --profile "{{tunnel_profile}}" --explain

tunnel-run:
    #!/usr/bin/env bash
    set -euo pipefail
    test -x "{{tunnel_client}}" || { echo "Install tunnel-client at {{tunnel_client}} or set TLDW_TUNNEL_CLIENT"; exit 1; }
    cleanup() {
        if [[ -n "${mcp_pid:-}" ]] && kill -0 "$mcp_pid" >/dev/null 2>&1; then
            kill "$mcp_pid" >/dev/null 2>&1 || true
            wait "$mcp_pid" >/dev/null 2>&1 || true
        fi
    }
    wait_for_mcp() {
        for _ in {1..50}; do
            if (:</dev/tcp/{{mcp_http_host}}/{{mcp_http_port}}) >/dev/null 2>&1; then
                return 0
            fi
            if ! kill -0 "$mcp_pid" >/dev/null 2>&1; then
                echo "tldw MCP server exited before {{mcp_server_url}} became reachable"
                return 1
            fi
            sleep 0.2
        done
        echo "Timed out waiting for {{mcp_server_url}}"
        return 1
    }
    trap 'cleanup' EXIT
    trap 'cleanup; exit 130' INT
    trap 'cleanup; exit 143' TERM
    {{mcp_http_command}} --port "{{mcp_http_port}}" &
    mcp_pid="$!"
    wait_for_mcp
    "{{tunnel_client}}" run --profile "{{tunnel_profile}}"

tunnel-launchd-install:
    /bin/bash "{{justfile_dir}}/scripts/tunnel-launchd" install \
        "{{launchd_label}}" \
        "{{launchd_keychain_service}}" \
        "{{tunnel_client}}" \
        "{{tunnel_profile}}" \
        "{{justfile_dir}}" \
        "{{mcp_http_command}}" \
        "{{mcp_http_host}}" \
        "{{mcp_http_port}}"

tunnel-launchd-uninstall:
    /bin/bash "{{justfile_dir}}/scripts/tunnel-launchd" uninstall "{{launchd_label}}"

tunnel-launchd-status:
    /bin/bash "{{justfile_dir}}/scripts/tunnel-launchd" status "{{launchd_label}}"

tunnel-launchd-logs mode="":
    /bin/bash "{{justfile_dir}}/scripts/tunnel-launchd" logs {{mode}}

help:
    @just --list
