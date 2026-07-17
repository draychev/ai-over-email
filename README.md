# ai-over-email

This service watches a Fastmail mailbox with JMAP, drafts replies through the OpenAI Responses API, sends the replies through Fastmail JMAP, and deletes processed inbound messages.

It can also run a second agent loop that watches a TLS NNTP newsgroup, sends posts and thread context to the same model pipeline, and posts Usenet follow-ups.

The same Fastmail JMAP layer can also be exposed as a local stdio MCP server for other agents and clients.

The mailbox persona may be called Pegasus as an homage to Pegasus Mail, the long-running email client from `pmail.com`. Pegasus Mail and the Mercury Mail Transport System can make a practical human-facing gateway for AI-over-email workflows; if you use them, support the project through the official manuals, support, or licensing options where applicable.

## Behavior

- Uses Fastmail JMAP EventSource for immediate mailbox state changes.
- Runs a startup and periodic inbox scan every 5 minutes so queued messages and missed notifications are still processed.
- Skips self-sent and automated/no-reply messages to avoid reply loops.
- Keeps a local SQLite correspondent profile database at `.tmp/correspondents.sqlite3`.
- Records each sender's email address, display name, derived email-header UTC offset when available, and whether a profile setup request was sent.
- Sends a one-time setup email to new correspondents asking for ZIP code and time zone when either value is missing.
- Limits each sender to 10 inbound messages per UTC day and sends a limit notice when they exceed it.
- Sends accepted replies as HTML email with a plain-text fallback.
- Preserves normal reply headers, quotes the original message, and reattaches original attachments.
- Sends image attachments to the model as image inputs and other attachments as file inputs when available.
- Requires OpenPGP encrypted and signed mail unless the sender is explicitly allowlisted in local credentials.
- Can watch `misc.pegasus` over NNTP/TLS as a separate Usenet responder service, preserving `References` and `In-Reply-To` headers when it posts follow-ups.

## Local Configuration

Runtime application config lives in local `config.json`, which is ignored and must not be committed. Copy `config.example.json` to `config.json` and edit local values there.

The `openai.powerful_senders` list can route selected sender addresses to `openai.powerful_model` with `openai.powerful_reasoning_effort`. Keep real sender addresses only in local `config.json`; use placeholders in the tracked example.

The `usenet` section configures the separate NNTP watcher. Set `security` to `tls` for implicit TLS on port 563, or `none` for authenticated plaintext NNTP on port 119. For self-signed TLS servers, use `tls_cert_sha256` to explicitly trust the certificate by fingerprint rather than disabling TLS verification.

Credentials are read from environment variables. For local development, copy `.env.example` to `.env` and put real values there. `.env` is ignored and must not be committed.

Supported environment variables:

```text
AI_OVER_EMAIL_FASTMAIL_TOKEN=<Fastmail JMAP API token>
AI_OVER_EMAIL_USERNAME=<mailbox address, required only for legacy password auth and outbound identity selection>
AI_OVER_EMAIL_OPENAI_API_KEY=<OpenAI API token>
AI_OVER_EMAIL_BRAVE_API_KEY=<Brave Search API token, optional; enables local Brave-backed web_search tool calls>
AI_OVER_EMAIL_MAILBOX=<mailbox name, optional; defaults to inbox>
AI_OVER_EMAIL_PUBLIC_EMAIL=<recipient address for PGP instructions, optional; defaults to AI_OVER_EMAIL_USERNAME>
AI_OVER_EMAIL_PLAINTEXT_ALLOWLIST=<comma-separated sender addresses allowed to send unencrypted mail>
AI_OVER_USENET_USERNAME=<NNTP username for the Usenet watcher>
AI_OVER_USENET_PASSWORD=<NNTP password for the Usenet watcher>
```

Keep personal addresses, credentials, API keys, access tokens, refresh tokens, and other secrets only in local untracked files.

## Commands

```sh
make test
make list
make mcp
make run
make run-usenet
```

## Fastmail MCP

Run a local stdio MCP server that exposes read-only Fastmail tools backed by the existing JMAP client:

```sh
make mcp
```

Current tools:

- `list_mailboxes`
- `search_messages`
- `get_message`

The MCP server reads the same local `.env` and `config.json` files as the watcher and mail listing commands.

## PGP Policy

Plaintext mail is only accepted from senders listed in `AI_OVER_EMAIL_PLAINTEXT_ALLOWLIST`. All other accepted messages must be OpenPGP messages that are encrypted to the configured recipient key and signed by the sender.

Rejected messages receive setup instructions instead of being sent to the model. The original rejected email is deleted after the rejection reply is sent.

## Systemd Service

The repo-tracked user service unit is:

```text
systemd/ai-over-email-mailwatch.service
systemd/ai-over-email-usenetwatch.service
```

After changing the unit file:

```sh
systemctl --user daemon-reload
systemctl --user restart ai-over-email-mailwatch.service
systemctl --user restart ai-over-email-usenetwatch.service
```

The unit reads production credentials from `%h/.config/ai-over-email/env`. Create that file with the same variable names shown in `.env.example` and restrict it with `chmod 600`.

## Verification

Run this after code changes:

```sh
go test ./...
go test -race ./...
go vet ./...
```
