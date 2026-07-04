#!/usr/bin/env bash
set -euo pipefail

src="${RSYNC_SOURCE:-de@10print:/home/de/src/ai-over-email/}"
dst="${RSYNC_DEST:-/home/de/src/ai-over-email/}"

mkdir -p "$dst"

exec rsync \
  --archive \
  --hard-links \
  --acls \
  --xattrs \
  --human-readable \
  --info=progress2 \
  "$src" \
  "$dst"
