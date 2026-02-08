#!/usr/bin/env bash
set -euo pipefail

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required env var: $name" >&2
    return 1
  fi
}

require_env SMTP_SERVER
require_env SMTP_PORT
require_env SMTP_USER
require_env SMTP_PASS
require_env FROM_EMAIL
require_env TO_EMAIL

SUBJECT=${SUBJECT:-"Test email from $(hostname)"}
BODY=${BODY:-"This is a test email sent at $(date -u '+%Y-%m-%d %H:%M:%S UTC')"}

if command -v swaks >/dev/null 2>&1; then
  swaks \
    --server "$SMTP_SERVER" \
    --port "$SMTP_PORT" \
    --tls \
    --auth LOGIN \
    --auth-user "$SMTP_USER" \
    --auth-password "$SMTP_PASS" \
    --from "$FROM_EMAIL" \
    --to "$TO_EMAIL" \
    --header "Subject: $SUBJECT" \
    --body "$BODY"
  exit 0
fi

if command -v curl >/dev/null 2>&1; then
  SMTP_URL=${SMTP_URL:-}
  SMTP_SCHEME=${SMTP_SCHEME:-smtp}

  if [[ -z "$SMTP_URL" ]]; then
    SMTP_URL="${SMTP_SCHEME}://${SMTP_SERVER}:${SMTP_PORT}"
  fi

  CURL_SSL_FLAG=""
  if [[ "$SMTP_SCHEME" == "smtp" ]]; then
    CURL_SSL_FLAG="--ssl-reqd"
  fi

  message_file=$(mktemp)
  trap 'rm -f "$message_file"' EXIT
  cat <<MSG > "$message_file"
From: $FROM_EMAIL
To: $TO_EMAIL
Subject: $SUBJECT

$BODY
MSG

  curl --silent --show-error --fail \
    --url "$SMTP_URL" \
    $CURL_SSL_FLAG \
    --mail-from "$FROM_EMAIL" \
    --mail-rcpt "$TO_EMAIL" \
    --user "$SMTP_USER:$SMTP_PASS" \
    -T "$message_file"

  exit 0
fi

echo "Neither swaks nor curl is available to send mail." >&2
exit 1
