#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$SCRIPT_DIR"

go run . \
  -send \
  -to "delqn@me.com" \
  -subject "This is a test from CODEX via MCP" \
  -body "Just build something"
