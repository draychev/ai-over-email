# Email Settings

Fastmail server settings from the Fastmail help article "Server names and ports":
https://www.fastmail.help/hc/en-us/articles/1500000278342-Server-names-and-ports

## Email

All email protocols require SSL/TLS. Use your full Fastmail email address as the username and a Fastmail app-specific password, not the regular account password.

| Protocol | Server | Port | Encryption | Notes |
| --- | --- | --- | --- | --- |
| IMAP | `imap.fastmail.com` | `993` | SSL/TLS enabled, not STARTTLS | Recommended over POP. Leave root folder / IMAP path prefix blank. Folder separator is `/`. |
| POP | `pop.fastmail.com` | `995` | SSL/TLS enabled, not STARTTLS | Pulls mail from the inbox by default. |
| SMTP | `smtp.fastmail.com` | `465` | SSL/TLS enabled, not STARTTLS | Authentication is PLAIN. |
| SMTP STARTTLS fallback | `smtp.fastmail.com` | `587` | STARTTLS enabled | Use only when a client supports STARTTLS only. |

Do not enable secure password authentication (SPA).

## Calendar, Contacts, and Files

| Service | Server | Encryption | Notes |
| --- | --- | --- | --- |
| CalDAV | `https://caldav.fastmail.com/` | SSL/TLS required | Calendar sync endpoint. |
| CardDAV | `https://carddav.fastmail.com/` | SSL/TLS required | Contacts sync endpoint. |
| WebDAV | `https://webdav.fastmail.com/` | SSL/TLS required | File access endpoint. Use an app-specific password with Files (WebDAV) access. |
| My Files WebDAV | `https://myfiles.fastmail.com/` | SSL/TLS required | Alternative WebDAV host rooted in the My Files directory. |

## JMAP

| Setting | Value | Notes |
| --- | --- | --- |
| JMAP session endpoint | `https://api.fastmail.com/jmap/session` | Current Fastmail API session endpoint. Use a JMAP API token as a bearer token. |
| JMAP legacy Basic auth session endpoint | `https://jmap.fastmail.com/.well-known/jmap` | Legacy session endpoint for username plus app-specific password Basic auth. Current Fastmail accounts should use an API token instead. |

For this repo, runtime JMAP endpoints live in `config.json`. Put the JMAP API token in `AI_OVER_EMAIL_FASTMAIL_TOKEN` through the process environment, a local `.env`, or the systemd environment file at `%h/.config/ai-over-email/env`. `AI_OVER_EMAIL_USERNAME` can still be used to document which mailbox the token belongs to and to select the outbound identity. `AI_OVER_EMAIL_MAILBOX` can override the watched mailbox; if it is omitted, the watcher uses the inbox.

## Firewall Proxy Servers

If firewall rules block standard ports, Fastmail provides SSL/TLS proxy hosts that can be used on any port. Commonly open ports include `80`, `21`, `25`, and `443`.

| Protocol | Proxy Server |
| --- | --- |
| IMAP | `imaps-proxy.fastmail.com` |
| POP | `pops-proxy.fastmail.com` |
| SMTP | `smtps-proxy.fastmail.com` |
