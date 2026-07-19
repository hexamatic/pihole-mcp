<div align="center">

<img src="assets/logo.svg" width="120" alt="pihole-mcp">

# pihole-mcp

A production-grade [MCP](https://modelcontextprotocol.io/) server for [Pi-hole](https://pi-hole.net/) v6.

**76+ tools** | **9 prompts** | **5 resources** | Multi-instance + sync | Single Go binary | 6.4 MB download (slim: 3.8 MB)

[![CI](https://github.com/hexamatic/pihole-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/hexamatic/pihole-mcp/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/hexamatic/pihole-mcp/graph/badge.svg)](https://codecov.io/gh/hexamatic/pihole-mcp)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/hexamatic/pihole-mcp/badge)](https://scorecard.dev/viewer/?uri=github.com/hexamatic/pihole-mcp)
[![Go Reference](https://pkg.go.dev/badge/github.com/hexamatic/pihole-mcp.svg)](https://pkg.go.dev/github.com/hexamatic/pihole-mcp)
[![Licence: MIT](https://img.shields.io/badge/licence-MIT-blue.svg)](LICENSE)

</div>

Gives AI assistants full control over your Pi-hole instance — DNS blocking, domain management, query analysis, statistics, network devices, DHCP, and system administration. Compatible with the Pi-hole v6 REST API.

## Quick Start

Most MCP clients use the same configuration format. Add this to your client's config:

```json
{
  "mcpServers": {
    "pihole": {
      "command": "pihole-mcp",
      "env": {
        "PIHOLE_URL": "http://192.168.1.2",
        "PIHOLE_PASSWORD": "your-password"
      }
    }
  }
}
```

Then install the binary via one of the methods below.

## Installation

### Homebrew

```bash
brew install hexamatic/tap/pihole-mcp
```

### Scoop (Windows)

```powershell
scoop bucket add hexamatic https://github.com/hexamatic/scoop-bucket
scoop install pihole-mcp
```

### Go Install

```bash
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@latest
```

### Docker

```bash
docker pull ghcr.io/hexamatic/pihole-mcp:latest
```

### Linux Packages

`.deb` and `.rpm` packages for Debian-based (Ubuntu, Raspberry Pi OS) and RPM-based (Fedora, RHEL) distributions are available on the [Releases](https://github.com/hexamatic/pihole-mcp/releases) page.

```bash
# Debian / Ubuntu / Raspberry Pi OS
sudo dpkg -i pihole-mcp_X.Y.Z_linux_amd64.deb

# Fedora / RHEL / CentOS
sudo rpm -i pihole-mcp_X.Y.Z_linux_amd64.rpm
```

### Binary Download

Pre-built binaries for Linux, macOS, and Windows (amd64 and arm64) are available on the [Releases](https://github.com/hexamatic/pihole-mcp/releases) page.

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PIHOLE_URL` | Yes | — | Pi-hole base URL (e.g. `http://192.168.1.2`) |
| `PIHOLE_PASSWORD` | Yes | — | Admin password or [application password](https://docs.pi-hole.net/api/auth/) |
| `PIHOLE_REQUEST_TIMEOUT` | No | `30s` | HTTP request timeout |
| `PIHOLE_MAX_RETRIES` | No | `3` | Retries after a failed Pi-hole API call. `0` disables. |
| `PIHOLE_RETRY_MAX_DELAY` | No | `8s` | Upper bound on a single backoff wait. |
| `PIHOLE_RATE_LIMIT` | No | `120` | Per-session requests-per-minute cap on the HTTP/SSE transports. `0` disables. |
| `PIHOLE_ALLOWED_ORIGINS` | No | `localhost,127.0.0.1,[::1]` | Comma-separated Origin/Host allowlist for HTTP/SSE transports. The literal `*` disables enforcement (unsafe). |
| `PIHOLE_TLS_SKIP_VERIFY` | No | `false` | Disable TLS certificate verification for Pi-hole connections. Only for instances serving self-signed certificates — prefer a trusted certificate where possible. |
| `TZ` | No | System timezone (UTC in Docker) | IANA timezone for rendered timestamps (e.g. `Australia/Adelaide`). Timezone data is embedded in the binary, so this works in the Docker image out of the box. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | — | OpenTelemetry collector endpoint. Setting it enables tracing; ignored in slim builds. |

Application passwords are recommended for automation — they bypass TOTP 2FA and can be revoked independently.

`PIHOLE_RATE_LIMIT` and `PIHOLE_ALLOWED_ORIGINS` only apply to the `http` and `sse` transports; stdio is a single-process, single-user channel by definition and isn't gated.

### Multiple instances

To manage more than one Pi-hole, configure numbered instances instead of `PIHOLE_URL`/`PIHOLE_PASSWORD`:

| Variable | Required | Description |
|----------|----------|-------------|
| `PIHOLE_1_URL`, `PIHOLE_2_URL`, … | Yes | Base URL of each instance (contiguous from 1) |
| `PIHOLE_1_PASSWORD`, `PIHOLE_2_PASSWORD`, … | Yes | Password for the matching instance |
| `PIHOLE_1_NAME`, `PIHOLE_2_NAME`, … | No | Friendly name (default `instance-1`, `instance-2`, …) |

```json
{
  "mcpServers": {
    "pihole": {
      "command": "pihole-mcp",
      "env": {
        "PIHOLE_1_URL": "http://192.168.1.2",
        "PIHOLE_1_PASSWORD": "primary-password",
        "PIHOLE_1_NAME": "downstairs",
        "PIHOLE_2_URL": "http://192.168.1.3",
        "PIHOLE_2_PASSWORD": "secondary-password",
        "PIHOLE_2_NAME": "upstairs"
      }
    }
  }
}
```

Every tool then accepts an optional `instance` argument, and every result is labelled with the instance it came from. Omit the argument to target the first instance; pass a name to target a specific one; pass `instance=all` on a read-only tool (e.g. `pihole_padd`, `pihole_stats_summary`) to query every instance concurrently and get back a single structured aggregate (per-instance results plus a success/failure summary — one slow or unreachable instance no longer fails the whole call). State-changing tools require a single named instance. `PIHOLE_URL` and `PIHOLE_1_URL` are mutually exclusive.

### Keeping instances in sync

When you run more than one Pi-hole, two extra tools appear for keeping them aligned:

- **`pihole_instance_diff`** — compare two instances and see exactly what differs across adlists/allowlists, allow/deny rules (exact and regex), groups, clients, local DNS A/AAAA records, and CNAME records. It is read-only and writes nothing.
- **`pihole_instance_sync`** — push a source instance's configuration onto a target. It is deliberately cautious:
  - **One direction only.** You name the `source` of truth and the `target`; only the target is ever written to.
  - **Dry-run first.** It returns a plan and a `confirm_token` by default; nothing changes until you re-run with `mode=apply` and that token. If the configuration drifts between planning and applying, the token no longer matches and the apply is refused.
  - **Add/update by default.** Entries on the target but not the source are left alone unless you pass `prune=true`.
  - **Backed up.** A teleporter backup of the target is taken before any change (disable with `snapshot=false`).
  - **Safe by omission.** Host-specific and identity settings — DHCP, interface bindings, passwords, TLS certificates, sessions, 2FA — are never synced. Group *membership* associations are not synced either, because Pi-hole group IDs are local to each instance.

Example: preview what the `upstairs` Pi-hole is missing relative to `downstairs`, then apply it.

```text
pihole_instance_diff   { "source": "downstairs", "target": "upstairs" }
pihole_instance_sync   { "source": "downstairs", "target": "upstairs" }            → returns a plan + confirm_token
pihole_instance_sync   { "source": "downstairs", "target": "upstairs",
                         "mode": "apply", "confirm_token": "<token from the plan>" }
```

## Client Setup

The Quick Start config above works for most clients. Expand the section below for client-specific instructions.

<details>
<summary><strong>Claude Desktop</strong></summary>

Add to your Claude Desktop configuration file:

| OS | Path |
|----|------|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |
| Linux | `~/.config/Claude/claude_desktop_config.json` |

```json
{
  "mcpServers": {
    "pihole": {
      "command": "pihole-mcp",
      "env": {
        "PIHOLE_URL": "http://192.168.1.2",
        "PIHOLE_PASSWORD": "your-password"
      }
    }
  }
}
```

Restart Claude Desktop after saving.

</details>

<details>
<summary><strong>Claude Code</strong></summary>

```bash
claude mcp add pihole \
  -e PIHOLE_URL=http://192.168.1.2 \
  -e PIHOLE_PASSWORD=your-password \
  -- pihole-mcp
```

Verify with:

```bash
claude mcp list
```

</details>

<details>
<summary><strong>VS Code (GitHub Copilot)</strong></summary>

Add to `.vscode/mcp.json` in your workspace:

```json
{
  "servers": {
    "pihole": {
      "type": "stdio",
      "command": "pihole-mcp",
      "env": {
        "PIHOLE_URL": "http://192.168.1.2",
        "PIHOLE_PASSWORD": "your-password"
      }
    }
  }
}
```

Or add via the command palette: `MCP: Add Server`.

> **Note:** VS Code uses `"servers"` as the top-level key (not `"mcpServers"`), and requires `"type": "stdio"`.

</details>

<details>
<summary><strong>Cursor</strong></summary>

Add to `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "pihole": {
      "command": "pihole-mcp",
      "env": {
        "PIHOLE_URL": "http://192.168.1.2",
        "PIHOLE_PASSWORD": "your-password"
      }
    }
  }
}
```

</details>

<details>
<summary><strong>Windsurf</strong></summary>

Add to `~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "pihole": {
      "command": "pihole-mcp",
      "env": {
        "PIHOLE_URL": "http://192.168.1.2",
        "PIHOLE_PASSWORD": "your-password"
      }
    }
  }
}
```

</details>

<details>
<summary><strong>Cline</strong></summary>

Open Cline settings > MCP Servers > Configure, and add:

```json
{
  "mcpServers": {
    "pihole": {
      "command": "pihole-mcp",
      "env": {
        "PIHOLE_URL": "http://192.168.1.2",
        "PIHOLE_PASSWORD": "your-password"
      }
    }
  }
}
```

</details>

<details>
<summary><strong>Docker (any client)</strong></summary>

For clients that support Docker-based MCP servers:

```json
{
  "mcpServers": {
    "pihole": {
      "command": "docker",
      "args": ["run", "-i", "--rm",
        "-e", "PIHOLE_URL=http://192.168.1.2",
        "-e", "PIHOLE_PASSWORD=your-password",
        "-e", "TZ=Australia/Adelaide",
        "ghcr.io/hexamatic/pihole-mcp:latest"]
    }
  }
}
```

Useful when you don't have Go installed or want to run the server on a remote host.

</details>

## Tools

A single Pi-hole exposes 76 tools. Configuring more than one adds `pihole_instance_diff` and `pihole_instance_sync`, for 78 — they are registered only when there is a second instance to compare against, so a single-Pi-hole setup isn't shown tools it cannot use.

The tables below are a summary; the full generated reference with every parameter is in [docs/TOOLS.md](docs/TOOLS.md).

### Dashboard
| Tool | Description |
|------|-------------|
| `pihole_padd` | One-call snapshot: queries, blocking, top domain/client, cache, versions, host health |

### DNS Control
| Tool | Description |
|------|-------------|
| `pihole_dns_get_blocking` | Get current DNS blocking status and timer |
| `pihole_dns_set_blocking` | Enable/disable blocking with optional timer |

### Statistics
| Tool | Description |
|------|-------------|
| `pihole_stats_summary` | Queries, blocking rate, clients, gravity size |
| `pihole_stats_top_domains` | Top queried or blocked domains |
| `pihole_stats_top_clients` | Most active clients by query count |
| `pihole_stats_upstreams` | Upstream DNS server performance |
| `pihole_stats_query_types` | Query type distribution (A, AAAA, MX, etc.) |
| `pihole_stats_recent_blocked` | Recently blocked domains |
| `pihole_stats_database` | Long-term database statistics |

### Domain Management
| Tool | Description |
|------|-------------|
| `pihole_domains_list` | List allow/deny domains |
| `pihole_domains_add` | Add domains (bulk supported) |
| `pihole_domains_update` | Update domain entry |
| `pihole_domains_delete` | Remove a domain |
| `pihole_domains_batch_delete` | Remove multiple domains |

### Groups, Clients, Lists
| Tool | Description |
|------|-------------|
| `pihole_groups_list/add/update/delete/batch_delete` | Manage groups |
| `pihole_clients_list/suggestions/add/update/delete` | Manage clients |
| `pihole_lists_list/add/update/delete/batch_delete` | Manage blocklists/allowlists |

### Query Log
| Tool | Description |
|------|-------------|
| `pihole_queries_search` | Search queries with 12 filters + cursor pagination |
| `pihole_queries_suggestions` | Available filter values |

### System
| Tool | Description |
|------|-------------|
| `pihole_info_system` | Host, CPU, memory, disk, load, temperature |
| `pihole_info_version` | Pi-hole component versions |
| `pihole_info_database` | Database size and query count |
| `pihole_info_messages` | FTL diagnostic messages |
| `pihole_info_dismiss_message` | Dismiss a diagnostic message by ID |
| `pihole_search_domains` | Cross-list domain search |
| `pihole_config_get/set` | Read/modify Pi-hole configuration |
| `pihole_config_get_value/add_value/remove_value` | Granular dotted-path config access |
| `pihole_config_properties` | List read-only config keys (Pi-hole v6.6.1+) |

### Actions and Network
| Tool | Description |
|------|-------------|
| `pihole_action_gravity_update` | Re-download blocklists |
| `pihole_action_restart_dns` | Restart FTL DNS resolver |
| `pihole_action_flush_logs/network` | Flush logs or network table |
| `pihole_network_devices/gateway/info` | Network device discovery |
| `pihole_dhcp_leases/delete_lease` | DHCP lease management |
| `pihole_logs_dns/ftl/webserver` | Log retrieval |
| `pihole_teleporter_export/import` | Configuration backup and restore |
| `pihole_history_graph/clients` | Activity history |

### Multi-instance (only with more than one Pi-hole configured)
| Tool | Description |
|------|-------------|
| `pihole_instance_diff` | Compare configuration between two instances |
| `pihole_instance_sync` | Reconcile a target instance towards a source (dry-run plan, then confirmed apply) |

### Response Options

Most tools accept optional parameters for controlling output:

- **`detail`** (`minimal` | `normal` | `full`) — Controls response depth. Default: `normal`. Use `minimal` for one-line summaries, `full` for complete API data.
- **`format`** (`text` | `csv`) — Output format for tabular data. Default: `text`. CSV saves ~29% tokens. Available on `pihole_domains_list`, `pihole_lists_list`, `pihole_clients_list`, `pihole_queries_search`, `pihole_network_devices`, `pihole_stats_top_domains`, `pihole_stats_top_clients`, `pihole_stats_upstreams`, `pihole_stats_query_types`, `pihole_stats_recent_blocked`, `pihole_stats_database_top_domains`, `pihole_stats_database_top_clients`, `pihole_stats_database_upstreams`, `pihole_dhcp_leases`, and `pihole_config_properties`.

## Prompts

Pre-built multi-step workflows for common tasks:

| Prompt | Description |
|--------|-------------|
| `diagnose_slow_dns` | Analyse upstream performance and identify bottlenecks |
| `investigate_domain` | Check why a domain is blocked/allowed across all lists |
| `review_top_blocked` | Identify false positives in top blocked domains |
| `audit_network` | Discover unknown devices and unconfigured clients |
| `optimise_blocklists` | Suggest list consolidation and cleanup |
| `daily_report` | Comprehensive daily Pi-hole health summary |
| `security_audit` | Review active sessions and auth config for unauthorised access |
| `weekly_trends` | Compare DNS statistics week over week |
| `upstream_health` | Deep performance analysis of upstream resolvers |

## Resources

Read-only context an MCP client can pull in without calling a tool:

| URI | Description |
|-----|-------------|
| `pihole://status` | Blocking status, version, health |
| `pihole://summary` | Query statistics |
| `pihole://clients/{client}` | Configuration and groups for one client |
| `pihole://domains/{type}/{kind}` | Domains on a list, e.g. `deny/exact` |
| `pihole://lists/{address}` | Details of one blocklist or allowlist |

With more than one Pi-hole configured, each instance is also addressable directly — `pihole://instances` lists them, and `pihole://<instance>/status` and `pihole://<instance>/summary` read a named one. The unprefixed URIs above always read the first-declared instance.

## Advanced Configuration

### Transport

By default, pihole-mcp uses stdio (standard for MCP). HTTP and SSE transports are also available:

```bash
# Default stdio (for Claude Desktop, Cursor, etc.)
pihole-mcp

# HTTP transport (for web-based MCP clients)
pihole-mcp -transport http -address localhost:8080

# SSE transport (deprecated — see below)
pihole-mcp -transport sse -address localhost:8080
```

> **SSE is deprecated.** The MCP specification superseded the HTTP+SSE transport with Streamable HTTP in the 2025-03-26 revision. `-transport sse` is kept for older clients and still receives security fixes, but new deployments should use `-transport http`. It will be removed once the clients that need it have moved on.

### Security (HTTP and SSE transports)

The `http` and `sse` transports apply two security middlewares to every request, in line with the MCP 2025-11-25 spec's DNS-rebinding protection guidance. stdio is unaffected (single-process, single-user).

- **Origin and Host validation.** Both headers must resolve to a host in `PIHOLE_ALLOWED_ORIGINS` (default loopback only). Missing `Origin` is allowed for non-browser MCP clients. Mismatches return HTTP 403. To expose pihole-mcp on a LAN, extend the allowlist:

  ```bash
  export PIHOLE_ALLOWED_ORIGINS="localhost,127.0.0.1,[::1],pihole-mcp.lan"
  ```

  The literal `*` disables enforcement entirely — only use it if you're behind a reverse proxy doing its own access control.

- **Per-session rate limiting.** A token bucket keyed by `Mcp-Session-Id` (fallback to client IP) caps requests at `PIHOLE_RATE_LIMIT` per minute (default `120`, burst `max(120/4, 30)`). Throttled requests return HTTP 429 with `Retry-After: 1`. `0` disables.

  ```bash
  # Tighter limit for a small fleet
  export PIHOLE_RATE_LIMIT=60

  # Disable (only when running behind a proxy with its own rate limit)
  export PIHOLE_RATE_LIMIT=0
  ```

### OpenTelemetry

Tracing is opt-in. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
pihole-mcp
```

All tool calls are automatically traced with tool name, duration, and error status.

If you don't need tracing, the slim build strips the OpenTelemetry SDK, gRPC, protobuf and grpc-gateway dependencies entirely — a little over 40% smaller:

| linux/amd64, v0.6.0 | Binary | Download (`.tar.gz`) | Docker image |
|---|---|---|---|
| Default | 16.4 MB | 6.1 MB | 18.2 MB |
| Slim | 9.2 MB | 3.6 MB | 11.8 MB |

```bash
just build-slim
# or
go build -tags slim -o bin/pihole-mcp-slim ./cmd/pihole-mcp

# Docker
docker pull ghcr.io/hexamatic/pihole-mcp:latest-slim
```

The slim binary is functionally identical apart from `OTEL_EXPORTER_OTLP_ENDPOINT` being ignored.

## Troubleshooting

### "Pi-hole rejected the login: its API session pool is full"

Pi-hole allows a limited number of concurrent API sessions — `webserver.api.max_sessions`, **16 by default** — and every client that logs in takes a seat: the web interface, PADD, Home Assistant, any other integration, and pihole-mcp. When they are all taken, Pi-hole answers `429` and refuses further logins, including from its own web interface.

pihole-mcp releases its seat on shutdown, but a session left behind by a process that was killed rather than stopped will hold one until it expires. Three ways out, in order of preference:

1. **Free a seat.** Ask for the session list (`pihole_auth_sessions`) and revoke one that is idle (`pihole_auth_revoke_session`).
2. **Raise the cap.** On a machine with a handful of integrations, 16 is low:
   ```bash
   pihole-FTL --config webserver.api.max_sessions 32
   ```
3. **Wait.** Seats release themselves after `webserver.session.timeout` — 30 minutes by default.

Retrying will not help, so pihole-mcp does not: it reports the problem instead of silently stalling.

### Authentication fails with a correct password

Pi-hole rate-limits repeated failed logins, and the limiter does not distinguish between "wrong password" and "the password you just fixed". Wait a few seconds and try again. If it persists, confirm you are using the admin password or an [application password](https://docs.pi-hole.net/api/auth/) — not the web interface's TOTP code.

### Docker: "connection refused" reaching Pi-hole

`localhost` inside a container is the container, not the host. Point `PIHOLE_URL` at the host's LAN address (`http://192.168.1.2`), at `host.docker.internal` on Docker Desktop, or put both containers on the same Docker network and use the Pi-hole container's name.

### Timestamps are shown in UTC

Every timestamp in tool output carries an explicit zone marker (e.g. `19 Jul 2026, 9:41 AM UTC`), so responses are unambiguous whatever the zone. Which zone is used depends on where the server runs: native binaries use the system timezone, while the Docker image defaults to UTC. To get local times from the container, set `TZ` on the *pihole-mcp* container (not just the Pi-hole one) — timezone data is embedded in the binary, so no extra packages or volume mounts are needed:

```yaml
environment:
  - TZ=Australia/Adelaide
```

An unrecognised `TZ` value logs a warning at startup and falls back to UTC rather than refusing to start.

### "x509: certificate signed by unknown authority"

Your Pi-hole is serving HTTPS with a self-signed certificate, which fails standard TLS verification. The right fix is a trusted certificate on the Pi-hole (for example via its built-in domain settings or a reverse proxy with Let's Encrypt). If that isn't practical, set `PIHOLE_TLS_SKIP_VERIFY=true` to disable verification — connections are still encrypted, but the server's identity is no longer checked, so only use this on a network you control.

### Occasional dropped connections

Pi-hole's embedded web server closes connections under load. pihole-mcp retries these automatically with backoff; if you see failures anyway, raise `PIHOLE_MAX_RETRIES` (default `3`).

## Development

```bash
# Prerequisites: Go 1.26+, Docker, mise, just

# One-command setup
just setup

# Start local Pi-hole (http://localhost:8081, password: test)
just dev-up

# Run quality checks (format + lint + test)
just check

# Run integration tests against local Pi-hole
just integration

# Build binary
just build
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for full development guidelines.

---

[Pi-hole](https://pi-hole.net/) is a registered trademark of Pi-hole LLC. This project is independently maintained and is not affiliated with, endorsed by, or sponsored by Pi-hole LLC.

## Licence

[MIT](LICENSE)
