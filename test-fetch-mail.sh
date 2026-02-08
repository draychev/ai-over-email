#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$SCRIPT_DIR"

go run . -n 50 | jq 'map(.body |= (if . == null then "" else (if (length > 25) then .[0:25] + "..." else . end) end))'
