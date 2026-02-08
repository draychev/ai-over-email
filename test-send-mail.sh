#!/usr/bin/env bash
set -euo pipefail
set -x

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$SCRIPT_DIR"

if go run . \
  -send \
  -to "delqn@me.com" \
  -subject "This is a test from CODEX via MCP" \
  -body "Just build something"; then
  set +x
  echo "Send succeeded"
else
  status=$?
  set +x
  echo "Send failed with exit code $status" >&2
  exit $status
fi
