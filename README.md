# ai-over-email

This service watches a Fastmail mailbox with JMAP, drafts replies through the OpenAI Responses API, sends the replies through Fastmail JMAP, and deletes processed inbound messages.

The implementation was incorporated from the working `pegasus-ai` service, with personal mailbox details moved out of committed source and into local credentials.

## Behavior

- Uses Fastmail JMAP EventSource for immediate mailbox state changes.
- Runs a startup and periodic inbox scan every 5 minutes so queued messages and missed notifications are still processed.
- Skips self-sent and automated/no-reply messages to avoid reply loops.
- Sends accepted replies as HTML email with a plain-text fallback.
- Preserves normal reply headers, quotes the original message, and reattaches original attachments.
- Sends image attachments to the model as image inputs and other attachments as file inputs when available.
- Requires OpenPGP encrypted and signed mail unless the sender is explicitly allowlisted in local credentials.

## Local Configuration

Local credentials live in `creds.txt`. Treat that file and any editor backups as secrets; they must not be committed.

Supported credential fields:

```text
Token=<Fastmail JMAP API token>
Username=<mailbox address>
OpenAIAPIToken=<OpenAI API token>
BraveSearchAPIToken=<Brave Search API token, optional; enables local Brave-backed web_search tool calls>
Mailbox=<mailbox name, optional; defaults to inbox>
PublicEmail=<recipient address for PGP instructions, optional; defaults to Username>
PlaintextAllowlist=<comma-separated sender addresses allowed to send unencrypted mail>
```

Keep personal addresses, credentials, API keys, access tokens, refresh tokens, and other secrets only in local untracked files.

## Commands

```sh
make test
make list
make run
```

## PGP Policy

Plaintext mail is only accepted from senders listed in `PlaintextAllowlist`. All other accepted messages must be OpenPGP messages that are encrypted to the configured recipient key and signed by the sender.

Rejected messages receive setup instructions instead of being sent to the model. The original rejected email is deleted after the rejection reply is sent.

## Systemd Service

The repo-tracked user service unit is:

```text
systemd/ai-over-email-mailwatch.service
```

After changing the unit file:

```sh
systemctl --user daemon-reload
systemctl --user restart ai-over-email-mailwatch.service
```

## Verification

Run this after code changes:

```sh
go test ./...
go test -race ./...
go vet ./...
```
