<div align="center">

<img src="assets/logo.svg" width="120" alt="pihole-mcp">

# pihole-mcp

A production-grade [MCP](https://modelcontextprotocol.io/) server for [Pi-hole](https://pi-hole.net/) v6.

**74 tools** | **9 prompts** | **5 resources** | Single Go binary | ~6MB compressed (slim: ~3.5MB)

[![Licence: MIT](https://img.shields.io/badge/licence-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/hexamatic/pihole-mcp)](https://goreportcard.com/report/github.com/hexamatic/pihole-mcp)
[![CI](https://github.com/hexamatic/pihole-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/hexamatic/pihole-mcp/actions/workflows/ci.yml)

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

### Go Install

```bash
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@latest
```

### Docker

```bash
docker pull ghcr.io/hexamatic/pihole-mcp:latest
```

### Binary Download

Pre-built binaries for Linux, macOS, and Windows (amd64 and arm64) are available on the [Releases](https://github.com/hexamatic/pihole-mcp/releases) page.

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PIHOLE_URL` | Yes | — | Pi-hole base URL (e.g. `http://192.168.1.2`) |
| `PIHOLE_PASSWORD` | Yes | — | Admin password or [application password](https://docs.pi-hole.net/api/auth/) |
| `PIHOLE_REQUEST_TIMEOUT` | No | `30s` | HTTP request timeout |
| `PIHOLE_RATE_LIMIT` | No | `120` | Per-session requests-per-minute cap on the HTTP/SSE transports. `0` disables. |
| `PIHOLE_ALLOWED_ORIGINS` | No | `localhost,127.0.0.1,[::1]` | Comma-separated Origin/Host allowlist for HTTP/SSE transports. The literal `*` disables enforcement (unsafe). |

Application passwords are recommended for automation — they bypass TOTP 2FA and can be revoked independently.

`PIHOLE_RATE_LIMIT` and `PIHOLE_ALLOWED_ORIGINS` only apply to the `http` and `sse` transports; stdio is a single-process, single-user channel by definition and isn't gated.

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
        "ghcr.io/hexamatic/pihole-mcp:latest"]
    }
  }
}
```

Useful when you don't have Go installed or want to run the server on a remote host.

</details>

## Tools

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

## Advanced Configuration

### Transport

By default, pihole-mcp uses stdio (standard for MCP). HTTP and SSE transports are also available:

```bash
# Default stdio (for Claude Desktop, Cursor, etc.)
pihole-mcp

# HTTP transport (for web-based MCP clients)
pihole-mcp -transport http -address localhost:8080

# SSE transport
pihole-mcp -transport sse -address localhost:8080
```

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

If you don't need tracing, you can build a slim binary that strips the OpenTelemetry SDK, gRPC, protobuf, and grpc-gateway dependencies entirely (~45% smaller — from 17 MB to 9 MB stripped, 6 MB to 3.5 MB compressed):

```bash
just build-slim
# or
go build -tags slim -o bin/pihole-mcp-slim ./cmd/pihole-mcp
```

The slim binary is functionally identical apart from `OTEL_EXPORTER_OTLP_ENDPOINT` being ignored.

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
