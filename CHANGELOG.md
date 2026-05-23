# Changelog

All notable changes to `pihole-mcp` are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The release body on GitHub for each tagged version is sourced from the matching section in this file. The `[Unreleased]` section accumulates user-visible changes between releases.

## [Unreleased]

### Added

- HTTP transport now ships with two security middlewares applied to the `http` and `sse` transports (stdio is unaffected): a per-session token-bucket rate limiter and an Origin/Host validator that protects against DNS-rebinding attacks per the MCP 2025-11-25 spec recommendation. Both are configurable via new environment variables (see Configuration).
- New `PIHOLE_RATE_LIMIT` env var. Defaults to `120` (requests per minute per session). `0` disables.
- New `PIHOLE_ALLOWED_ORIGINS` env var. Comma-separated list. Defaults to `localhost,127.0.0.1,[::1]`. The literal `*` disables enforcement (documented as unsafe).
- Input validation at handler entry for `pihole_domains_*`, `pihole_lists_*`, `pihole_clients_*`, `pihole_groups_*`, and `pihole_config_*` tools. Domain names are checked for RFC 1035 compliance (length, labels, no shell metacharacters); list URLs must parse as http/https/file with a non-empty host or path; comments and free-form names are length-capped. Invalid inputs return a friendly MCP error before any API call is made, instead of surfacing a raw 400 from the Pi-hole server.
- **`pihole_config_properties`** — new tool that lists configuration keys locked as read-only by `pihole.toml` or environment variable, with reason and description. Useful after a `pihole_config_set` rejection to confirm whether a key is intentionally immutable. Requires Pi-hole FTL v6.6.1+; the handler surfaces a friendly "endpoint requires Pi-hole FTL v6.6.1+" error against older releases. Tool count is now 74.
- `format=csv` parameter added to `pihole_stats_recent_blocked`, `pihole_stats_query_types`, `pihole_stats_upstreams`, `pihole_stats_database_upstreams`, and `pihole_dhcp_leases`. Renders the same data as a comma-separated table — a ~30-40% token saving for callers that don't need the narrative summary.

### Changed

- The `http` and `sse` transports now run inside a `net/http.Server` constructed by `cmd/pihole-mcp/main.go` rather than mcp-go's built-in `.Start()` helper. This is what allows the middleware chain to wrap the MCP handler. Behaviourally identical for clients that respect the existing graceful-shutdown signal handling.
- `pihole_history_graph` / `_history_clients` / `_history_database` / `_history_database_clients` descriptions now lead with the data source ("in-memory" vs "database") and cross-reference each other. Removes the cognitive overhead of working out which tool you want from name alone.
- `pihole_network_info` description clarified to point users to `pihole_network_routes` / `pihole_network_interfaces` for richer per-route or per-interface detail.
- `pihole_config_set` is now annotated `openWorldHint: true`. The tool can affect DNS resolution and other services system-wide; the hint surfaces that to MCP clients that gate destructive operations.

### Fixed

- Hardened `pihole_network_devices` against invalid UTF-8 bytes in the upstream `macVendor` field (Pi-hole FTL upstream issue [#2868](https://github.com/pi-hole/FTL/issues/2868)). Go's `encoding/json` already silently replaces non-UTF-8 sequences with U+FFFD during decode, so this MCP server was unaffected — a regression-prevention test is now in place to lock that behaviour in.

### Dependencies

- `golang.org/x/time` v0.15.0 added as a direct dependency to back the rate-limit token bucket.

- `github.com/mark3labs/mcp-go` bumped 0.47.0 → 0.54.0. Brings panic recovery to the SSE message handler, stdio worker, task goroutines, and session hook goroutines; adds a transport-agnostic `Handle` entry point; adds OpenTelemetry server-side tracing hooks; adds `WithStrictInputSchemaDefault`. No breaking changes for our usage — all `server.NewMCPServer`, `server.NewStreamableHTTPServer`, and `server.NewSSEServer` call sites compile and pass tests unchanged.

## [v0.3.0] - 2026-05-07

### Highlights

Five new tools complete the network-management and long-term-history surfaces, raising tool coverage to 73. A fixture-based test infrastructure was introduced and immediately surfaced three Pi-hole API shape bugs that had been silently masking incorrect data — all now corrected. Conventional Commits and `CHANGELOG.md` updates are now enforced both locally and in CI, and releases publish straight to GitHub Releases without a manual draft step.

### Added

- **`pihole_history_database`** — query the long-term FTL database for total query counts grouped by interval. `from`/`until` parameters scope the window and default to the last 7 days when omitted.
- **`pihole_history_database_clients`** — per-client breakdown of long-term query history over the same windowed interval.
- **`pihole_network_routes`** — list every routing-table entry observed by FTL with family, scope, and source attribution.
- **`pihole_network_interfaces`** — list network interfaces with link state, speed, addresses, and per-interface byte counters.
- **`pihole_network_delete_device`** — remove a stale device record from the FTL network table.
- Fixture-based testing harness under `testdata/fixtures/` (13 captured Pi-hole responses) plus a `loadFixture(t, name)` helper in `internal/tools/fixtures_test.go`. Fixture refresh is automated via `scripts/refresh-fixtures.sh` and the `just refresh-fixtures` recipe.
- `RELEASING.md` runbook documenting the tag-driven release procedure.

### Changed

- `pihole_history_graph` and `pihole_history_clients` now operate exclusively on in-memory FTL data. The overloaded `from`/`until` parameters that previously routed those tools through the long-term database have been removed — that path now lives in the dedicated `pihole_history_database*` tools above.
- `internal/pihole/types.go` — `DatabaseInfo`, `NetworkInterface`, and `NetworkInterfaceStats` were updated to match the actual Pi-hole v6 wire format observed in fixtures (see Fixed below).

### Fixed

- **`/api/info/database`** — the response is flat at the top level, not wrapped in a `database` key. The previous shape silently returned all-zeros for SQLite version, file size, and timestamp fields. `DatabaseInfo` is now flat and the affected handler renders correct values.
- **`info_database.earliest_timestamp`** — Pi-hole emits `float64` (sub-second precision), not `int64`. Type updated; previous decode path discarded fractional seconds and could fail on values that exceeded the int range.
- **`/api/network/interfaces[].speed`** — nullable for loopback and tunnel interfaces. Now `*int`; previous non-pointer field caused JSON decode errors against real Pi-hole instances.
- **`/api/network/interfaces[].addresses[].prefixlen`** — corrected from `prefix` to `prefixlen` to match the Pi-hole field name.
- **`/api/network/interfaces[].stats.rx_bytes` / `tx_bytes`** — wire format is `{unit, value}` envelopes, not raw integers. Type updated and `format.Bytes()` rendering applies.

### Release Pipeline

- `.goreleaser.yaml` now sets `release.draft: false` and `release.mode: keep-existing`. Tags publish straight to GitHub Releases with no manual draft step, and re-runs do not overwrite already-published bodies.
- Conventional Commits are enforced locally via a lefthook `commit-msg` hook (zero deps, pure shell regex) and on PRs via `wagoid/commitlint-github-action`.
- `CHANGELOG.md` updates are enforced on PRs via `dangoslen/changelog-enforcer`. The `Skip-Changelog` label exists for legitimately internal-only PRs (refactors, tests, CI, dev tooling, no-op dependency bumps).
- `scripts/release-notes.sh` extracts each release body from this file and feeds it to goreleaser via `--release-notes=NOTES.md`, replacing the auto-generated changelog block.
- `scripts/changelog-draft.sh` scaffolds the next `[Unreleased]` section from `git log` when prepping a release.

### Migration Notes

- **`pihole_history_graph` and `pihole_history_clients`** no longer accept `from` / `until` parameters. Long-term database queries that previously routed through these tools are now served by the dedicated `pihole_history_database` and `pihole_history_database_clients` tools, both of which default to a 7-day window when `from` / `until` are omitted.

### Installation

**Go install:**
```
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.3.0
```

**Docker (multi-arch):**
```
docker pull ghcr.io/hexamatic/pihole-mcp:0.3.0
```

**Binary download:** grab the archive for your platform from the release assets.

### Requirements

- Pi-hole v6.6+ with the REST API enabled
- An admin password or [application password](https://docs.pi-hole.net/api/auth/)

### Configuration

| Variable                  | Required | Default | Description                       |
| ------------------------- | -------- | ------- | --------------------------------- |
| `PIHOLE_URL`              | Yes      | —       | Pi-hole base URL                  |
| `PIHOLE_PASSWORD`         | Yes      | —       | Admin or application password     |
| `PIHOLE_REQUEST_TIMEOUT`  | No       | `30s`   | HTTP request timeout              |

See the [README](https://github.com/hexamatic/pihole-mcp#readme) for client-specific setup guides (Claude Desktop, Cursor, Windsurf, VS Code, Cline) and the [OpenTelemetry section](https://github.com/hexamatic/pihole-mcp#opentelemetry) for opt-in tracing configuration.

## [v0.2.0] - 2026-05-02

### Highlights

The repository moved from `lloydmcl/pihole-mcp` to `hexamatic/pihole-mcp` — existing GitHub URLs redirect, stars, watchers, issues, and forks remain in place; new container images publish to `ghcr.io/hexamatic/pihole-mcp`. This release also lands the v0.2.0 feature set: twelve new tools (68 total, ~95% Pi-hole v6 REST API coverage), three new MCP prompts (nine total), structured output schemas on key tools per the MCP 2025-11-25 specification, and sixteen new unit-test files raising coverage across all 17 tool categories.

### Added

- **Long-term database statistics** — four new tools surface historical analysis over the FTL database with mandatory `from`/`until` windows:
  - `pihole_stats_database_top_domains`
  - `pihole_stats_database_top_clients`
  - `pihole_stats_database_upstreams`
  - `pihole_stats_database_query_types`
- **Granular configuration** — three new tools support dotted-path access into Pi-hole's hierarchical config and deferred FTL restart for batch operations:
  - `pihole_config_get_value`
  - `pihole_config_add_value`
  - `pihole_config_remove_value`
- **System info** — three new tools expose FTL engine internals, live DNS/DHCP metrics, and hardware temperature sensors (`pihole_info_ftl`, `pihole_info_metrics`, `pihole_info_sensors`).
- **Session management** — two new tools for security auditing: `pihole_auth_sessions` (list active sessions) and `pihole_auth_revoke_session` (terminate a session by ID).
- **MCP prompts** — three new prompts:
  - `security_audit` — reviews active sessions, authentication configuration, and diagnostic messages for unauthorised access patterns.
  - `weekly_trends` — week-over-week DNS statistics comparison via the new long-term database tools.
  - `upstream_health` — DNS resolver performance, cache efficiency, and DNSSEC validation analysis.
- **Structured output schemas** — `pihole_dns_get_blocking`, `pihole_stats_summary`, and `pihole_domains_list` now return structured output per MCP 2025-11-25, allowing downstream agents to parse results programmatically without LLM interpretation of formatted text.

### Changed

- **Repository transferred** from `lloydmcl/pihole-mcp` to `hexamatic/pihole-mcp`. Existing GitHub URLs redirect; the Go module path, container image references, badge URLs, and goreleaser configuration are rewritten to the new namespace across `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, the issue templates, and every Go source file.
- **Container images** now publish to `ghcr.io/hexamatic/pihole-mcp` (multi-arch: linux/amd64, linux/arm64).
- **Project branding** — replaced the Pi-hole Vortex logo with custom branding to avoid trademark conflicts.
- **Shared helpers** extracted across tool handlers (`toolError`, `getCountCapped`) centralise error formatting and count clamping that were previously duplicated.

### Fixed

- **`justfile` PATH composition** — `mise bin-paths` emits one path per line; the previous concatenation truncated `PATH` at the first entry, leaving `goreleaser` and other mise-managed tools unreachable from `just release-dry`. Now collapses newlines into the colon separator before prepending.

### Quality

- Sixteen new unit-test files raise tool-handler coverage across all 17 tool categories, including error paths and parameter validation.
- The E2E suite is extended to cover the twelve new tools and a config add/remove round-trip against a live Pi-hole.

### Migration Notes

- **Container image path** — pull from `ghcr.io/hexamatic/pihole-mcp:0.2.0` (or `:latest`). The previous `ghcr.io/lloydmcl/pihole-mcp` path is no longer published; existing images at the old path remain accessible but receive no updates.
- **Go module path** — `go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.2.0`. Existing imports of `github.com/lloydmcl/pihole-mcp` should be updated; the GitHub redirect handles the source pull, but Go's module proxy may cache under the new path.

### Installation

**Go install:**
```
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.2.0
```

**Docker (multi-arch):**
```
docker pull ghcr.io/hexamatic/pihole-mcp:0.2.0
```

**Binary download:** grab the archive for your platform from the release assets.

### Requirements

- Pi-hole v6.6+ with the REST API enabled
- An admin password or [application password](https://docs.pi-hole.net/api/auth/)

### Configuration

| Variable                  | Required | Default | Description                       |
| ------------------------- | -------- | ------- | --------------------------------- |
| `PIHOLE_URL`              | Yes      | —       | Pi-hole base URL                  |
| `PIHOLE_PASSWORD`         | Yes      | —       | Admin or application password     |
| `PIHOLE_REQUEST_TIMEOUT`  | No       | `30s`   | HTTP request timeout              |

See the [README](https://github.com/hexamatic/pihole-mcp#readme) for client-specific setup guides (Claude Desktop, Cursor, Windsurf, VS Code, Cline) and the [OpenTelemetry section](https://github.com/hexamatic/pihole-mcp#opentelemetry) for opt-in tracing configuration.

## [v0.1.0] - 2026-04-06

### Highlights

A production-grade MCP server for Pi-hole v6, providing complete API coverage through 55 tools, 6 prompts, and 5 resources — all in a single Go binary.

### Added

- **55 tools** across 16 categories: DNS blocking, statistics, queries, domains, groups, clients, lists, config, actions, network, DHCP, logs, and more.
- **6 MCP prompts** for guided workflows: DNS diagnosis, domain investigation, blocked domain review, network audit, blocklist optimisation, and daily reporting.
- **5 MCP resources** for quick status checks (`pihole://status`, `pihole://summary`, plus three URI templates for client/domain/list detail).
- **Response controls** — `detail` (`minimal`/`normal`/`full`) and `format` (`text`/`csv`) parameters on applicable tools, letting callers trade verbosity for token economy.
- **Session lifecycle management** — lazy login on first call, automatic re-authentication on 401 with compare-and-swap to avoid thundering herd, and session cleanup on shutdown to prevent FTL session-slot exhaustion.
- **Optional OpenTelemetry tracing** — opt-in via `OTEL_EXPORTER_OTLP_ENDPOINT` for end-to-end observability.
- **Transports** — stdio (default), HTTP (Streamable HTTP), and SSE.

### Installation

**Go install:**
```
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.1.0
```

**Docker (multi-arch):**
```
docker pull ghcr.io/hexamatic/pihole-mcp:0.1.0
```

> **Note:** v0.1.0 was released under `lloydmcl/pihole-mcp` and `ghcr.io/lloydmcl/pihole-mcp`. Both URLs still resolve via GitHub redirects; the `hexamatic` paths above point at the same artefacts going forward. See v0.2.0 migration notes.

**Binary download:** grab the archive for your platform from the release assets.

### Requirements

- Pi-hole v6.6+ with the REST API enabled
- An admin password or [application password](https://docs.pi-hole.net/api/auth/)

### Configuration

| Variable                  | Required | Default | Description                       |
| ------------------------- | -------- | ------- | --------------------------------- |
| `PIHOLE_URL`              | Yes      | —       | Pi-hole base URL                  |
| `PIHOLE_PASSWORD`         | Yes      | —       | Admin or application password     |
| `PIHOLE_REQUEST_TIMEOUT`  | No       | `30s`   | HTTP request timeout              |

See the [README](https://github.com/hexamatic/pihole-mcp#readme) for client-specific setup guides (Claude Desktop, Cursor, Windsurf, VS Code, Cline).

[Unreleased]: https://github.com/hexamatic/pihole-mcp/compare/v0.3.0...HEAD
[v0.3.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/hexamatic/pihole-mcp/releases/tag/v0.1.0
