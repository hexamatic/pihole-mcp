<div align="center">

<img src="assets/logo.svg" width="120" alt="pihole-mcp">

# pihole-mcp

A production-grade [MCP](https://modelcontextprotocol.io/) server for [Pi-hole](https://pi-hole.net/) v6.

**82 tools** (84 with a second instance) | **9 prompts** | **5 resources** | Multi-instance + sync | Read-only mode + toolsets | Single Go binary

[![CI](https://github.com/hexamatic/pihole-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/hexamatic/pihole-mcp/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/hexamatic/pihole-mcp/graph/badge.svg)](https://codecov.io/gh/hexamatic/pihole-mcp)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/hexamatic/pihole-mcp/badge)](https://scorecard.dev/viewer/?uri=github.com/hexamatic/pihole-mcp)
[![Go Reference](https://pkg.go.dev/badge/github.com/hexamatic/pihole-mcp.svg)](https://pkg.go.dev/github.com/hexamatic/pihole-mcp)
[![Licence: MIT](https://img.shields.io/badge/licence-MIT-blue.svg)](LICENSE)

</div>

Gives AI assistants full control over your Pi-hole instance: DNS blocking, domain management, query analysis, statistics, network devices, DHCP, and system administration. Compatible with the Pi-hole v6 REST API.

## Why this one

- **Run it locked down.** `PIHOLE_READ_ONLY=true` exposes only the tools that cannot change anything, enforced on every call, not just the tool list a client happens to cache. `PIHOLE_TOOLSETS` narrows further to just the families you need, so a client asking "what can I use" gets a smaller, cheaper answer.
- **82 tools** (84 once a second Pi-hole is configured) covering roughly 95% of the Pi-hole v6 REST API, including domains, lists, groups, clients, DHCP, network, the query log, configuration, local DNS records, teleporter backup and restore, and session management.
- **Multi-instance diff and sync.** Compare two Pi-holes and see exactly what differs, or reconcile one onto the other with a dry-run plan and a confirm token before anything is written.
- **Releases are signed and attested, not just built.** Every archive and container image carries a keyless cosign signature, an SPDX SBOM and SLSA build provenance, and [SECURITY.md](SECURITY.md) has the one-line command to check each.
- **Listed in the official [MCP Registry](https://registry.modelcontextprotocol.io/)** as `io.github.hexamatic/pihole-mcp`, installable by name from any client that supports registry install.
- **Shipped nine releases in five months** since launch, each with real release notes describing what changed and why, not a commit-hash dump.

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

## What you can ask

The whole point is natural language, not tool names. A few examples of what to actually type:

| You ask | What runs |
|---|---|
| "Is Pi-hole blocking right now, and for how long?" | `pihole_dns_get_blocking` |
| "Pause blocking for 10 minutes, I need to test something." | `pihole_dns_set_blocking` |
| "Why is reddit.com blocked? Check every list it could be on." | `pihole_search_domains` |
| "What are the top blocked domains today, and does anything look like a false positive?" | `pihole_stats_recent_blocked`, the `review_top_blocked` prompt |
| "Point nas.home at 192.168.1.50." | `pihole_local_dns_add` |
| "Which of my blocklists haven't updated in 30+ days?" | `pihole_lists_list` |
| "Show me devices on the network Pi-hole doesn't recognise." | `pihole_network_devices`, the `audit_network` prompt |
| "Compare my two Pi-holes and tell me what's different." | `pihole_instance_diff` |
| "Back up my configuration before I change anything." | `pihole_teleporter_export` |
| "Which upstream resolver is slowest, and is it worth switching?" | `pihole_stats_upstreams`, the `upstream_health` prompt |

Set `PIHOLE_READ_ONLY=true` first if you want to ask questions without any risk of something being
changed. See [Scoping the tool surface](#scoping-the-tool-surface).

## Installation

### MCP Registry

pihole-mcp is listed in the [official MCP Registry](https://registry.modelcontextprotocol.io/) as:

```
io.github.hexamatic/pihole-mcp
```

Clients that support registry install can add it by that name and will prompt for
`PIHOLE_URL` and `PIHOLE_PASSWORD`. The listing points at the `ghcr.io` image, so the
client needs a working Docker.

### Homebrew

```bash
brew install hexamatic/tap/pihole-mcp
```

Installs on both macOS and Linux (Homebrew on Linux). On macOS the cask clears the
quarantine attribute during install, so the binary runs without a Gatekeeper prompt.

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

Releases are checksummed, signed with keyless cosign, and ship SPDX SBOMs and SLSA build provenance. See [SECURITY.md](SECURITY.md#verifying-release-artefacts) for the verification commands.

### Check your setup

Once installed and configured, run the server directly with `-check`. It loads the configuration, contacts every Pi-hole you have configured, and reports each one by name:

```bash
PIHOLE_URL=http://192.168.1.2 PIHOLE_PASSWORD=your-password pihole-mcp -check
```

```text
pihole-mcp 0.9.0

PASS  primary (http://192.168.1.2)  core v6.1.4, FTL v6.7

All 1 instance(s) reachable and authenticated.
```

It exits non-zero if any instance fails, and prints what to change. Worth running before you wire the server into a client: most clients hide the server's output, so a bad URL or password looks the same as a server that never started.

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PIHOLE_URL` | Yes | - | Pi-hole base URL (e.g. `http://192.168.1.2`) |
| `PIHOLE_PASSWORD` | Yes | - | Admin password or [application password](https://docs.pi-hole.net/api/auth/) |
| `PIHOLE_READ_ONLY` | No | `false` | Expose only tools that cannot change Pi-hole. See [Scoping the tool surface](#scoping-the-tool-surface). |
| `PIHOLE_TOOLSETS` | No | all | Comma-separated toolset names to expose, e.g. `dashboard,domains`. Unset or `all` exposes everything. |
| `PIHOLE_REQUEST_TIMEOUT` | No | `30s` | HTTP request timeout |
| `PIHOLE_MAX_RETRIES` | No | `3` | Retries after a failed Pi-hole API call. `0` disables. |
| `PIHOLE_RETRY_MAX_DELAY` | No | `8s` | Upper bound on a single backoff wait. |
| `PIHOLE_HTTP_AUTH_TOKEN` | No | - | Shared bearer token required on every HTTP/SSE request. Minimum 16 characters. Unset means no authentication. |
| `PIHOLE_HTTP_AUTH_TOKEN_FILE` | No | - | Path to a file holding the bearer token, so it stays out of `ps`. Mutually exclusive with `PIHOLE_HTTP_AUTH_TOKEN`. |
| `PIHOLE_RATE_LIMIT` | No | `120` | Per-session requests-per-minute cap on the HTTP/SSE transports, under a per-address ceiling of four times that. `0` disables. |
| `PIHOLE_ALLOWED_ORIGINS` | No | `localhost,127.0.0.1,[::1]` | Comma-separated Origin/Host allowlist for HTTP/SSE transports. The literal `*` disables enforcement (unsafe). Not authentication. |
| `PIHOLE_TRUSTED_PROXIES` | No | - | Comma-separated IPs or CIDR blocks whose `X-Forwarded-For` the rate limiter believes. Unset means the header is ignored. |
| `PIHOLE_TLS_SKIP_VERIFY` | No | `false` | Disable TLS certificate verification for Pi-hole connections. Only for instances serving self-signed certificates. A trusted certificate is preferable where possible. |
| `TZ` | No | System timezone (UTC in Docker) | IANA timezone for rendered timestamps (e.g. `Australia/Adelaide`). Timezone data is embedded in the binary, so this works in the Docker image out of the box. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | - | OpenTelemetry collector endpoint. Setting it enables tracing; ignored in slim builds. |

Application passwords are recommended for automation: they bypass TOTP 2FA and can be revoked independently.

`PIHOLE_HTTP_AUTH_TOKEN`, `PIHOLE_HTTP_AUTH_TOKEN_FILE`, `PIHOLE_RATE_LIMIT`, `PIHOLE_ALLOWED_ORIGINS` and `PIHOLE_TRUSTED_PROXIES` only apply to the `http` and `sse` transports; stdio is a single-process, single-user channel by definition and isn't gated. See [Security](#security-http-and-sse-transports) before exposing either transport beyond loopback.

### Scoping the tool surface

By default the server offers every tool, including 34 that change Pi-hole and 27 that are
destructive. Two variables narrow that.

`PIHOLE_READ_ONLY=true` exposes only tools that cannot change anything. Write tools are absent from
`tools/list` **and** rejected if called anyway, so a client working from a cached listing cannot
reach one either.

```bash
PIHOLE_READ_ONLY=true    # 49 tools instead of 82
```

`PIHOLE_TOOLSETS` selects which families of tools exist at all, which is worth doing on its own
because `tools/list` is a cost paid in every conversation:

```bash
PIHOLE_TOOLSETS=dashboard,domains,lists    # blocklist curation, and nothing else
PIHOLE_TOOLSETS=all                        # the default, stated explicitly
```

The two intersect, and read-only always wins. `PIHOLE_READ_ONLY=true` with
`PIHOLE_TOOLSETS=dns,stats` gives the 14 tools that are in those toolsets *and* read-only.

The toolset names, what each contains and how many tools it holds are in
[docs/TOOLS.md](docs/TOOLS.md#toolsets), generated from the same table the server filters with. An
unknown name fails at startup and lists every valid one; run `pihole-mcp -check` to see it, along
with how many tools your configuration actually exposes:

```
scope: read-only=true, toolsets=dns,stats  14 of 82 tools exposed
```

A published toolset name is never removed, renamed or narrowed. See
[CONTRIBUTING.md](CONTRIBUTING.md#toolset-stability) for the full promise.

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

Every tool then accepts an optional `instance` argument, and every result is labelled with the instance it came from. Omit the argument to target the first instance; pass a name to target a specific one; pass `instance=all` on a read-only tool (e.g. `pihole_padd`, `pihole_stats_summary`) to query every instance concurrently and get back a single structured aggregate (per-instance results plus a success/failure summary, so one slow or unreachable instance no longer fails the whole call). State-changing tools require a single named instance. `PIHOLE_URL` and `PIHOLE_1_URL` are mutually exclusive.

### Keeping instances in sync

When you run more than one Pi-hole, two extra tools appear for keeping them aligned:

- **`pihole_instance_diff`**: compare two instances and see exactly what differs across adlists/allowlists, allow/deny rules (exact and regex), groups, clients, local DNS A/AAAA records, and CNAME records. It is read-only and writes nothing.
- **`pihole_instance_sync`**: push a source instance's configuration onto a target. It is deliberately cautious:
  - **One direction only.** You name the `source` of truth and the `target`; only the target is ever written to.
  - **Dry-run first.** It returns a plan and a `confirm_token` by default; nothing changes until you re-run with `mode=apply` and that token. If the configuration drifts between planning and applying, the token no longer matches and the apply is refused.
  - **Add/update by default.** Entries on the target but not the source are left alone unless you pass `prune=true`.
  - **Backed up.** A teleporter backup of the target is taken before any change (disable with `snapshot=false`).
  - **Safe by omission.** Host-specific and identity settings (DHCP, interface bindings, passwords, TLS certificates, sessions, 2FA) are never synced. Group *membership* associations are not synced either, because Pi-hole group IDs are local to each instance.

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

If the server does not appear, run `pihole-mcp -check` in a terminal with the same environment variables to see whether it can reach your Pi-hole.

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

If the server does not appear, run `pihole-mcp -check` in a terminal with the same environment variables to see whether it can reach your Pi-hole.

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

If the server does not appear, run `pihole-mcp -check` in a terminal with the same environment variables to see whether it can reach your Pi-hole.

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

If the server does not appear, run `pihole-mcp -check` in a terminal with the same environment variables to see whether it can reach your Pi-hole.

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

If the server does not appear, run `pihole-mcp -check` in a terminal with the same environment variables to see whether it can reach your Pi-hole.

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

If the server does not appear, run `pihole-mcp -check` in a terminal with the same environment variables to see whether it can reach your Pi-hole.

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
        "-v", "/host/path/for/backups:/backups",
        "ghcr.io/hexamatic/pihole-mcp:latest"]
    }
  }
}
```

Useful when you don't have Go installed or want to run the server on a remote host.

The `-v` mount is there for `pihole_teleporter_export`. Without it, a backup is written inside the container's own filesystem and is gone as soon as `--rm` removes the container on exit. Pass `output_path` pointing inside the mount (e.g. `/backups/pihole.zip`) to keep it on the host.

To check the container can reach your Pi-hole, run the same image with `-check`. Remember that `localhost` inside a container is the container itself, which is the usual cause of a failure here:

```bash
docker run --rm \
  -e PIHOLE_URL=http://192.168.1.2 \
  -e PIHOLE_PASSWORD=your-password \
  ghcr.io/hexamatic/pihole-mcp:latest -check
```

</details>

## Tools

A single Pi-hole exposes 82 tools. Configuring more than one adds `pihole_instance_diff` and `pihole_instance_sync`, for 84. Those two are registered only when there is a second instance to compare against, so a single-Pi-hole setup isn't shown tools it cannot use.

To expose fewer, see [Scoping the tool surface](#scoping-the-tool-surface).

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

### Local DNS
| Tool | Description |
|------|-------------|
| `pihole_local_dns_list` | List local A and AAAA records (`dns.hosts`) |
| `pihole_local_dns_add` | Point a hostname at an IP address; the record type follows the address family |
| `pihole_local_dns_delete` | Remove a local A or AAAA record by IP address and hostname |
| `pihole_local_cname_list` | List local CNAME records (`dns.cnameRecords`) |
| `pihole_local_cname_add` | Point an alias at another name, with an optional TTL |
| `pihole_local_cname_delete` | Remove a local CNAME record by its alias |

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
| `pihole_stats_database_top_domains/top_clients/upstreams/query_types` | Long-term database equivalents of the four tools above, for an arbitrary date range |

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
| `pihole_clients_list/suggestions/add/update/delete/batch_delete` | Manage clients |
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
| `pihole_info_client` | The requesting client's own IP and connection info |
| `pihole_info_ftl` | FTL process info: PID, privacy level, client/domain counts |
| `pihole_info_metrics` | Live DNS and DHCP operational metrics (cache, inserts, evictions, leases) |
| `pihole_info_sensors` | Hardware temperature sensors |
| `pihole_search_domains` | Cross-list domain search |
| `pihole_auth_sessions/revoke_session` | List and revoke active API sessions |
| `pihole_config_get/set` | Read/modify Pi-hole configuration |
| `pihole_config_get_value/add_value/remove_value` | Granular dotted-path config access |
| `pihole_config_properties` | List read-only config keys (Pi-hole v6.6.1+) |

### Actions and Network
| Tool | Description |
|------|-------------|
| `pihole_action_gravity_update` | Re-download blocklists |
| `pihole_action_restart_dns` | Restart FTL DNS resolver |
| `pihole_action_flush_logs` | Flush the query log |
| `pihole_action_flush_network` | Flush the network table (Pi-hole v6.3+) |
| `pihole_network_devices/gateway/info/routes/interfaces` | Network device and interface discovery |
| `pihole_network_delete_device` | Remove a stale network device record |
| `pihole_dhcp_leases/delete_lease` | DHCP lease management |
| `pihole_logs_dns/ftl/webserver` | Log retrieval |
| `pihole_teleporter_export/import` | Configuration backup and restore |
| `pihole_history_graph/clients` | Activity history (in-memory) |
| `pihole_history_database/database_clients` | Activity history (long-term database, arbitrary date range) |

### Multi-instance (only with more than one Pi-hole configured)
| Tool | Description |
|------|-------------|
| `pihole_instance_diff` | Compare configuration between two instances |
| `pihole_instance_sync` | Reconcile a target instance towards a source (dry-run plan, then confirmed apply) |

### Response Options

Some tools accept optional parameters for controlling output:

- **`detail`** (`minimal` | `normal` | `full`): controls response depth. Default: `normal`. Use `minimal` for one-line summaries, `full` for complete API data. Available on 15 tools.
- **`format`** (`text` | `csv`): output format for tabular data. Default: `text`. CSV saves roughly 8–23% tokens over the default text rendering, depending on the tool and row count. Available on 22 tools: `pihole_domains_list`, `pihole_groups_list`, `pihole_lists_list`, `pihole_clients_list`, `pihole_queries_search`, `pihole_network_devices`, `pihole_dhcp_leases`, `pihole_config_properties`, `pihole_local_dns_list`, `pihole_local_cname_list`, `pihole_stats_top_domains`, `pihole_stats_top_clients`, `pihole_stats_upstreams`, `pihole_stats_query_types`, `pihole_stats_recent_blocked`, `pihole_stats_database_top_domains`, `pihole_stats_database_top_clients`, `pihole_stats_database_upstreams`, `pihole_history_graph`, `pihole_history_clients`, `pihole_history_database`, and `pihole_history_database_clients`.

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

With more than one Pi-hole configured, each instance is also addressable directly: `pihole://instances` lists them, and `pihole://<instance>/status` and `pihole://<instance>/summary` read a named one. The unprefixed URIs above always read the first-declared instance.

## Advanced Configuration

### Transport

By default, pihole-mcp uses stdio (standard for MCP). HTTP and SSE transports are also available:

```bash
# Default stdio (for Claude Desktop, Cursor, etc.)
pihole-mcp

# HTTP transport (for web-based MCP clients)
pihole-mcp -transport http -address localhost:8080

# SSE transport (deprecated, see below)
pihole-mcp -transport sse -address localhost:8080
```

Both examples bind loopback, so only this machine can reach them. Before changing that, read
[Security](#security-http-and-sse-transports) below: neither transport authenticates anything until
you configure a token.

pihole-mcp implements MCP protocol revision **2025-11-25** and negotiates down for older clients
(2025-06-18, 2025-03-26 and 2024-11-05 are all accepted).

> **SSE is deprecated.** The HTTP+SSE transport was superseded by Streamable HTTP in the 2025-03-26
> revision, and formally reclassified as **Deprecated** under the specification's feature lifecycle
> policy ([SEP-2596](https://modelcontextprotocol.io/specification/2026-07-28/changelog)), meaning
> it stays part of the spec, fully supported, for a minimum of twelve months before removal could
> even be proposed. `-transport sse` is kept for older clients and still receives security fixes,
> but new deployments should use `-transport http`.

### Security (HTTP and SSE transports)

**The `http` and `sse` transports have no authentication until you configure one.** stdio is
unaffected: it is a single-process, single-user channel by definition.

The default listen address is `localhost:8080`, so out of the box only this machine can reach the
server. Change the bind address without setting a token and anyone who can reach the port can
control your Pi-hole: disable blocking, rewrite your blocklists, read every DNS query on the
network.

- **Bearer authentication (recommended whenever the bind address is not loopback).** Set a shared
  token and every request must carry it in an `Authorization: Bearer` header. Requests without it
  are answered `401` before anything else runs. Tokens must be at least 16 characters.

  ```bash
  export PIHOLE_HTTP_AUTH_TOKEN="$(openssl rand -base64 32)"
  # A bind beyond loopback needs the allowlist extended too, or clients reach a
  # 403 from the Host check before the token is ever looked at.
  export PIHOLE_ALLOWED_ORIGINS="localhost,127.0.0.1,[::1],192.168.1.10"
  pihole-mcp -transport http -address 0.0.0.0:8080
  ```

  An environment variable set on a command line is visible in `ps` to every user on the host, so
  the token can be read from a file instead. This is also the shape container platforms mount
  secrets in:

  ```bash
  export PIHOLE_HTTP_AUTH_TOKEN_FILE=/run/secrets/pihole-mcp-token
  ```

  Set one or the other, not both. The token is compared in constant time, so a wrong token tells
  an attacker nothing about how much of it was right. Rejected requests are charged to a separate
  failed-authentication budget of roughly twenty attempts per address and then one a second, so the
  token cannot be guessed at line rate, a flood of wrong tokens cannot starve a client at the same
  address that does hold it, and the server logs the throttling once rather than once per attempt.

  Binding a non-loopback address with no token logs an error at startup and carries on. It does not
  refuse to start, because putting a reverse proxy that authenticates in front of it is a
  legitimate deployment.

- **Origin and Host validation.** Both headers must resolve to a host in `PIHOLE_ALLOWED_ORIGINS`
  (default loopback only). Mismatches return `403`. Missing `Origin` is allowed, because non-browser
  MCP clients do not send one.

  **This is not authentication.** It is DNS-rebinding protection, and it only protects browsers: a
  page a victim visits cannot make their browser attach an `Origin` the allowlist accepts. Both
  headers are chosen by the caller, so any client that is not a browser sets them to whatever the
  allowlist wants. Use `PIHOLE_HTTP_AUTH_TOKEN` for access control.

  ```bash
  export PIHOLE_ALLOWED_ORIGINS="localhost,127.0.0.1,[::1],pihole-mcp.lan"
  ```

  The literal `*` disables enforcement entirely. That is reasonable behind a reverse proxy, or with
  a bearer token set, and not otherwise.

- **Rate limiting.** A token bucket per client address, with a second bucket per MCP session
  underneath it. `PIHOLE_RATE_LIMIT` (default `120`, burst `max(120/4, 30)`) is the per-session
  rate; one address may send four times that. Throttled requests return `429` with `Retry-After: 1`.
  `0` disables.

  The address ceiling is what actually bounds a caller, because `Mcp-Session-Id` is a header the
  client writes: without a ceiling, a caller sending a fresh session id on every request would never
  be limited at all. The header is also being removed from the transport in MCP revision
  `2026-07-28`, after which the ceiling is the whole limiter. Every request is charged to the
  ceiling first, including one the session bucket then rejects, so a client that is already being
  throttled keeps drawing on the budget its neighbours share. IPv6 clients are counted per `/64`,
  since a single host is routinely given a whole one.

  ```bash
  # Tighter limit for a small fleet
  export PIHOLE_RATE_LIMIT=60

  # Disable (only when running behind a proxy with its own rate limit)
  export PIHOLE_RATE_LIMIT=0
  ```

- **Behind a reverse proxy.** By default the client address is the connection's peer address, which
  behind a proxy is the proxy itself, so every client through it shares one ceiling. Name the proxy
  networks and `X-Forwarded-For` is used instead, taking the rightmost entry that is not itself a
  listed proxy:

  ```bash
  export PIHOLE_TRUSTED_PROXIES="192.168.1.5"
  ```

  Nothing is trusted by default, deliberately: `X-Forwarded-For` is client-supplied, so a server
  that believes it unconditionally lets any caller pick its own rate-limit bucket. **Every host
  inside a prefix you list gets that power**, so name the individual proxies where you can and keep
  any CIDR block as tight as the deployment allows. `10.0.0.0/8` trusts your whole network.

### OpenTelemetry

Tracing is opt-in. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
pihole-mcp
```

All tool calls are automatically traced with tool name, duration, and error status.

If you don't need tracing, the slim build strips the OpenTelemetry SDK, gRPC, protobuf and grpc-gateway dependencies entirely. The result is 44% smaller:

| linux/amd64 binary | Size |
|---|---|
| Default | 17.5 MB |
| Slim | 9.8 MB (44% smaller) |

Measured from a local build; see the [Releases](https://github.com/hexamatic/pihole-mcp/releases) page for the exact archive and Docker image sizes of a given tag.

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

Pi-hole allows a limited number of concurrent API sessions (`webserver.api.max_sessions`, **16 by default**), and every client that logs in takes a seat: the web interface, PADD, Home Assistant, any other integration, and pihole-mcp. When they are all taken, Pi-hole answers `429` and refuses further logins, including from its own web interface.

pihole-mcp releases its seat on shutdown, but a session left behind by a process that was killed rather than stopped will hold one until it expires. Three ways out, in order of preference:

1. **Free a seat.** Ask for the session list (`pihole_auth_sessions`) and revoke one that is idle (`pihole_auth_revoke_session`).
2. **Raise the cap.** On a machine with a handful of integrations, 16 is low:
   ```bash
   pihole-FTL --config webserver.api.max_sessions 32
   ```
3. **Wait.** Seats release themselves after `webserver.session.timeout` (30 minutes by default).

Retrying will not help, so pihole-mcp does not: it reports the problem instead of silently stalling.

### Authentication fails with a correct password

Pi-hole rate-limits repeated failed logins, and the limiter does not distinguish between "wrong password" and "the password you just fixed". Wait a few seconds and try again. If it persists, confirm you are using the admin password or an [application password](https://docs.pi-hole.net/api/auth/), not the web interface's TOTP code.

### Docker: "connection refused" reaching Pi-hole

`localhost` inside a container is the container, not the host. Point `PIHOLE_URL` at the host's LAN address (`http://192.168.1.2`), at `host.docker.internal` on Docker Desktop, or put both containers on the same Docker network and use the Pi-hole container's name.

### Timestamps are shown in UTC

Every timestamp in tool output carries an explicit zone marker (e.g. `19 Jul 2026, 9:41 AM UTC`), so responses are unambiguous whatever the zone. Which zone is used depends on where the server runs: native binaries use the system timezone, while the Docker image defaults to UTC. To get local times from the container, set `TZ` on the *pihole-mcp* container (not just the Pi-hole one). Timezone data is embedded in the binary, so no extra packages or volume mounts are needed:

```yaml
environment:
  - TZ=Australia/Adelaide
```

An unrecognised `TZ` value logs a warning at startup and falls back to UTC rather than refusing to start.

### "x509: certificate signed by unknown authority"

Your Pi-hole is serving HTTPS with a self-signed certificate, which fails standard TLS verification. The right fix is a trusted certificate on the Pi-hole (for example via its built-in domain settings or a reverse proxy with Let's Encrypt). If that isn't practical, set `PIHOLE_TLS_SKIP_VERIFY=true` to disable verification. Connections are still encrypted, but the server's identity is no longer checked, so only use this on a network you control.

### Occasional dropped connections

Pi-hole's embedded web server closes connections under load. pihole-mcp retries these automatically with backoff; if you see failures anyway, raise `PIHOLE_MAX_RETRIES` (default `3`).

## Getting help

- **Usage question?** Ask in [Q&A Discussions](https://github.com/hexamatic/pihole-mcp/discussions/categories/q-a); someone with the same question later will find the answer there.
- **Found a bug, or want a feature?** Open an [issue](https://github.com/hexamatic/pihole-mcp/issues/new/choose).
- **Security vulnerability?** See [SECURITY.md](SECURITY.md). Please don't open a public issue.

See [SUPPORT.md](SUPPORT.md) for the full picture, including where to start contributing.

## Development

```bash
# Prerequisites: Go 1.26+, Docker, mise, just

# One-command setup
just setup

# Start local Pi-hole (http://localhost:8081, password: test)
# Port already taken? PIHOLE_DEV_PORT=8091 just dev-up
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
