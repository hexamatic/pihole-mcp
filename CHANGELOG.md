# Changelog

All notable changes to `pihole-mcp` are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The release body on GitHub for each tagged version is sourced from the matching section in this file. The `[Unreleased]` section accumulates user-visible changes between releases.

## [Unreleased]

### Added

- **Signed, attested releases.** Release artefacts are now signed with keyless [cosign](https://docs.sigstore.dev/) (checksum file and Docker images), ship SPDX SBOMs for every archive, and carry SLSA build provenance verifiable with `gh attestation verify`. [SECURITY.md](SECURITY.md#verifying-release-artefacts) documents every verification command.
- **OpenSSF Scorecard** — a weekly supply-chain security analysis now runs against the repository, publishes its score to [scorecard.dev](https://scorecard.dev/viewer/?uri=github.com/hexamatic/pihole-mcp), and feeds the new README badge. All GitHub Actions across every workflow are now pinned by commit SHA (maintained automatically by Dependabot).

### Changed

- **README badge lineup refreshed.** The Go Report Card badge has been removed — the service was sunset on 1 July 2026 — and replaced with OpenSSF Scorecard and Go Reference (pkg.go.dev) badges. Its lint-quality role has long been covered by golangci-lint in CI.

## [v0.7.0] - 2026-07-19

### Highlights

This release makes every timestamp unambiguous. Tool output used to render times with no timezone marker, in whatever zone the server happened to be running in — which, in the published Docker image, silently meant UTC: the distroless base ships no timezone database, so even setting `TZ` on the container did nothing ([#23](https://github.com/hexamatic/pihole-mcp/discussions/23)). **Every timestamp now carries an explicit zone marker, and the IANA timezone database is embedded in the binary itself**, so `TZ=Australia/Adelaide` on the container — or on any of the native binaries, Windows included — renders query logs in your local time out of the box. Left unset, output is UTC and says so.

Alongside that: an explicit opt-in for Pi-holes serving self-signed HTTPS certificates (`PIHOLE_TLS_SKIP_VERIFY`), and a round of hardening that has been on the list since v0.4.0 — coverage reporting, fuzz testing of the input validators, full-history secret scanning with gitleaks, and a [generated tool reference](https://github.com/hexamatic/pihole-mcp/blob/main/docs/TOOLS.md) that CI keeps honest so the docs can never again drift from the code.

### Added

- **`TZ` timezone support for rendered timestamps** ([#23](https://github.com/hexamatic/pihole-mcp/discussions/23)). Set `TZ` to an IANA zone (e.g. `Australia/Adelaide`) and every timestamp in tool output renders in that zone. The IANA timezone database is embedded in the binary, so this works in the Docker image — and on Windows — with no extra packages or volume mounts. An unrecognised `TZ` logs a startup warning and falls back rather than refusing to start.
- **`PIHOLE_TLS_SKIP_VERIFY`** (default `false`) — opt-in for Pi-hole instances serving self-signed HTTPS certificates. Verification stays on by default; the README documents why a trusted certificate is the better fix.
- **Full generated tool reference** at [docs/TOOLS.md](https://github.com/hexamatic/pihole-mcp/blob/main/docs/TOOLS.md) — every tool with its parameters, produced from the registered tool definitions by `cmd/toolsdoc` and checked for drift in CI, so it cannot go stale.
- **Coverage reporting** via Codecov on every push and pull request.
- **Secret scanning** with gitleaks, both as a pre-commit hook and as a full-history CI scan.
- **Fuzz testing** for the tool-parameter validators, run continuously in CI alongside the existing table-driven tests.

### Changed

- **Every timestamp now carries an explicit zone marker** (e.g. `19 Jul 2026, 9:41 AM UTC` rather than `19 Jul 2026, 9:41 AM`). Previously the Docker image silently rendered timestamps in unlabelled UTC — ambiguous for both people and AI agents trying to convert times ([#23](https://github.com/hexamatic/pihole-mcp/discussions/23)).

## [v0.6.0] - 2026-07-12

### Highlights

This release is about the things that go wrong. Pi-hole's embedded web server drops connections under load, and until now pihole-mcp passed that straight through — a dropped connection became a failed tool call. **Requests are now retried with exponential backoff**, but deliberately not uniformly: measured against FTL v6.7, the 429 you are actually most likely to meet is `api_seats_exceeded`, and it is not a rate limit at all. Pi-hole allows only **16 concurrent API sessions by default**, every client that logs in takes one, and a seat is freed only when a session times out half an hour later. Retrying it is futile, so pihole-mcp doesn't — it tells you how to fix it, pointing you at the tools that list and revoke sessions. This is the most frequently reported Pi-hole v6 API problem and it used to surface as an opaque error.

Two bugs surfaced from running the server against a real Pi-hole rather than a mock. **`pihole_info_messages` had never shown the text of a diagnostic message** — FTL returns it under `plain`, we read a key that does not exist, and every warning rendered as a type and a timestamp with nothing between them. It hid for five releases because a healthy Pi-hole reports no messages at all, so no test ever covered a populated response. And **configuring a second Pi-hole silently stripped the output schema from every tool that had one** — the flagship multi-instance feature quietly degrading the structured-output support it should have showcased.

On security: `golang.org/x/net` was carrying **seven advisories**, and the Go toolchain a reachable one in `crypto/tls`. All are fixed here. None of it was visible, because there was no vulnerability scanning in CI and every automated dependency update had been failing its checks since the day those checks were added. Both problems are now fixed, and `govulncheck` runs on every push, every pull request, and weekly — because advisories get disclosed against code that hasn't changed.

Verified end to end against **Pi-hole FTL v6.7**.

### Added

- **`pihole_info_dismiss_message`** — dismiss an FTL diagnostic message once you have dealt with it. Previously you could see Pi-hole's warnings but not clear them, so the `daily_report` and `security_audit` prompts had to send you to the web interface. `pihole_info_messages` now prints each message's ID to pass to it.
- **Automatic retry with exponential backoff and jitter** for transient Pi-hole failures. `PIHOLE_MAX_RETRIES` (default `3`, `0` disables) and `PIHOLE_RETRY_MAX_DELAY` (default `8s`).
- **A distinct, actionable error when Pi-hole's API session pool is full**, explaining that the limit is `webserver.api.max_sessions` (default 16), that every client takes a seat, and that you can free one with `pihole_auth_sessions` and `pihole_auth_revoke_session`.
- **Per-instance resources.** With more than one Pi-hole configured, each is addressable at `pihole://<instance>/status` and `pihole://<instance>/summary`, with a `pihole://instances` index. The unprefixed URIs still read the first-declared instance.
- README sections for **Troubleshooting** (session exhaustion, auth failures, Docker networking, dropped connections) and **Resources**, which had never been documented despite being advertised.

### Changed

- Verified against **Pi-hole FTL v6.7**; the development and CI containers now run `2026.07.2`.
- The SSE transport is **documented as deprecated**, in line with the MCP specification superseding HTTP+SSE with Streamable HTTP. It still works and still receives fixes; new deployments should use `-transport http`.
- Retries are **method-aware**. A rate-limited request is safe to replay for any method, because Pi-hole rejected it before processing it. A connection that failed without a reply is only replayed for reads — when we cannot know whether Pi-hole applied a delete, a duplicated delete is worse than the error it would have papered over.
- `just refresh-fixtures` now **seeds the development Pi-hole first**. A newly created Pi-hole has no query history, so every statistics endpoint answers empty and fixtures captured from it assert nothing.
- Documented artefact sizes are now **measured rather than remembered**. The long-repeated "9 MB Docker image" was wrong — that was roughly the binary size. Default: 16.4 MB binary, 6.1 MB download, 18.2 MB image. Slim: 9.2 MB, 3.6 MB, 11.8 MB.

### Fixed

- **`pihole_info_messages` displayed no message text.** FTL returns the body under `plain`; the client decoded a `message` key the API does not send, so every diagnostic warning rendered as a bare type and timestamp. Present since the tool was introduced.
- **Configuring a second Pi-hole stripped structured output from every tool.** A multi-instance tool can return either a single-instance result or an `instance=all` aggregate, and rather than describe both, the output schema was discarded entirely — so nine tools advertised a schema on one Pi-hole and none did on two. Both shapes are now declared as a `oneOf`.
- **A 5xx during login reported itself as "authentication failed".** A server that cannot answer is not a bad password.
- `DoRaw` and `PostMultipart` bypassed the 401 re-authentication path that every other request had.
- **Automated dependency updates had been failing CI since the checks were introduced.** The changelog enforcer skipped on labels that did not exist in the repository, so no bot pull request ever carried one; and commitlint's 100-character body limit tripped on the single unwrappable line Dependabot writes to name every module in a grouped update. Ungrouped updates passed, which made the failure look intermittent rather than systematic.
- Intermittent `sending auth request: EOF` failures in CI. The end-to-end suite starts a process per tool call, each authenticating afresh, which exhausts FTL's session table faster than a flat 0.3-second retry could outlast.
- `README` claimed nine prompts while listing six. `security_audit`, `weekly_trends` and `upstream_health` shipped in v0.2.0 and had been invisible to users ever since.

### Security

- **`golang.org/x/net` 0.52.0 → 0.55.0**, fixing GO-2026-4918, GO-2026-5025, GO-2026-5026, GO-2026-5027, GO-2026-5028, GO-2026-5029 and GO-2026-5030. **`golang.org/x/sys`** likewise fixes GO-2026-5024.
- **Go toolchain pinned to 1.26.5**, fixing GO-2026-5856 — an Encrypted Client Hello privacy leak in `crypto/tls` that `govulncheck` reports as reachable from the HTTP transport and the Pi-hole client's TLS paths.
- **`govulncheck` added to CI**, on every push and pull request plus a weekly schedule. The schedule is the point: advisories are disclosed against code that has not changed, so scanning only on push leaves a quiet repository silently vulnerable.

### Dependencies

- `github.com/mark3labs/mcp-go` 0.54.1 → 0.56.0
- `go.opentelemetry.io/otel` and friends 1.43.0 → 1.44.0
- `github.com/grpc-ecosystem/grpc-gateway/v2` 2.28.0 → 2.29.0
- `actions/checkout` 6 → 7
- Go toolchain 1.26.4 → 1.26.5

## [v0.5.0] - 2026-06-26

### Highlights

This release makes pihole-mcp a first-class tool for households and homelabs that run more than one Pi-hole. **Multiple instances** can now be configured side by side (`PIHOLE_1_URL`, `PIHOLE_2_URL`, …); every tool takes an optional `instance` argument, results are labelled with their source instance, and read-only tools accept `instance=all` to query the whole fleet concurrently and return a single structured aggregate. Two new tools turn the server into a safe alternative to standalone sync utilities: **`pihole_instance_diff`** reports exactly how two Pi-holes differ (adlists, allow/deny rules, groups, clients, local DNS), and **`pihole_instance_sync`** reconciles a target towards a source as a dry-run plan you confirm before anything is written — one direction only, host-specific and secret settings never touched. A new **`pihole_padd` dashboard tool** collapses what used to be half a dozen calls — queries, blocking state, top domain/client, cache, versions, and host health — into one snapshot, making it the natural first call for any status check. Alignment with the latest MCP best practices deepens across the board: every tool now carries a human-readable **title** with consistent behaviour hints, long-running actions emit **progress notifications**, the server emits **structured log messages** (with credential redaction), and the `investigate_domain` prompt offers **argument completions** sourced from your live domain rules. Tracks the current Pi-hole release (FTL v6.6.2 / docker `2026.05.0`) and toolchain (mcp-go v0.54.1).

### Added

- **Multi-instance support** — configure several Pi-holes with `PIHOLE_1_URL`/`PIHOLE_1_PASSWORD` (optional `PIHOLE_1_NAME`), `PIHOLE_2_URL`, and so on. The single-instance `PIHOLE_URL`/`PIHOLE_PASSWORD` form is unchanged and is named `primary`. Every tool gains an optional `instance` argument (advertised on the schema only when more than one instance is configured) and labels its result with the source instance. Read-only tools also accept `instance=all`, which now queries every instance **concurrently** and returns a **structured aggregate** (`summary` counts plus a per-instance array, each entry labelled and carrying its own data or error) alongside the `### instance: <name>` text fallback; a failure on one instance no longer fails the whole call. State-changing tools reject `instance=all`. The first-declared instance is the default and backs all resources.
- **`pihole_instance_diff`** — compare configuration between two instances (groups, adlists/allowlists, allow/deny exact and regex rules, clients, local DNS A/AAAA records, and CNAME records) and report what is added, changed, or only on the target. Read-only; runs no writes. Only registered when more than one instance is configured.
- **`pihole_instance_sync`** — reconcile a target Pi-hole towards a source, one direction only. Runs as a dry-run **plan** by default and returns a `confirm_token`; re-run with `mode=apply` and that token to apply. The token is derived from the planned changes, so a configuration that drifts between planning and applying is rejected rather than silently overwritten. Adds and updates by default; deletions require `prune=true`. A teleporter backup of the target is taken before any change (disable with `snapshot=false`). Host-specific and identity/secret settings (DHCP, interface bindings, passwords, TLS, sessions, 2FA) are never synced; group-membership associations are not synced because Pi-hole group IDs are instance-local.
- **`pihole_padd`** — a single-call dashboard snapshot (queries incl. `query_frequency`, blocking state, top domain/blocked/client, recent blocked, cache counters, FTL CPU/memory, CPU temperature, component versions, and — at `detail=full` — the primary network interface and host model). Structured output schema included. Recommended as the first call for a status overview.
- **Tool titles** — all 77 tools now set a human-readable `title` annotation (e.g. "Dashboard Snapshot", "Top Domains") distinct from the programmatic name, which MCP clients can surface in their UI.
- **Progress notifications** — `pihole_action_gravity_update` streams `notifications/progress` as the gravity rebuild proceeds (when the client supplies a `progressToken`); the flush actions emit start/complete progress.
- **MCP logging** — the server now emits `notifications/message` log events for notable operations (gravity update lifecycle, DNS restart, log/network flush, configuration changes), tagged with the originating instance. Credential-bearing fields are redacted before delivery, and the SDK gates delivery by the client's configured log level.
- **Prompt argument completions** — `completion/complete` is now supported; the `investigate_domain` prompt completes its `domain` argument from the configured allow/deny rules on the default instance.
- **Structured output schemas** added to `pihole_info_system`, `pihole_stats_top_domains`, and `pihole_stats_top_clients` (structured content is emitted even when `format=csv`).
- Development tooling for multi-instance work: a second Pi-hole behind the Compose `multi` profile, plus `just dev-up-multi` / `just dev-down-multi` and a multi-instance section in the E2E suite (gated on `PIHOLE_2_URL`).
- A Docker-free, in-process Pi-hole emulator (`internal/pihole/piholefake`) that backs the unit tests for routing, aggregation, and sync, plus a `just sim` walkthrough that runs the full plan→apply→converge flow locally without containers. CI now starts a second Pi-hole and runs the multi-instance integration tests and E2E suite against both.

### Fixed

- E2E harness: a parameter-default expansion (`${2:-{}}`) appended a stray `}` to every supplied tool-argument payload, so multi-instance E2E calls sent malformed JSON. The harness now also retries transient transport failures (a dropped connection under rapid-fire load is not a tool failure) and isolates the single- and multi-instance environment forms, making the suite a reliable CI gate.

### Changed

- Server instructions now recommend `pihole_padd` as the entry point and document the Pi-hole FTL v6.6 / v6.5 configuration keys (`resolver.macNames`, `database.forceDisk`, `dns.cache.rrtype`) that are settable via `pihole_config_set`.
- Tool count is now **77** (was 74): added `pihole_padd`, `pihole_instance_diff`, and `pihole_instance_sync` (the latter two appear only in multi-instance setups).
- Behaviour annotations are now internally consistent: read-only tools are no longer also flagged destructive or open-world (mcp-go's `NewTool` defaults both to true), so MCP clients render accurate hints. A test locks this invariant in across every tool.

### Dependencies

- Bumped `github.com/mark3labs/mcp-go` v0.54.0 → v0.54.1.
- Development and CI Pi-hole image pinned to `pihole/pihole:2026.05.0` (FTL v6.6.2 / Core v6.4.2 / Web v6.5).
- CI golangci-lint pinned v2.11 → v2.12; release workflow Docker actions bumped to v4 (Node 24 runtime).

## [v0.4.0] - 2026-05-23

### Highlights

This release hardens the HTTP and SSE transports so that pihole-mcp can safely be exposed beyond stdio — a per-session token-bucket rate limiter and an Origin/Host validator now wrap every HTTP and SSE request, matching the DNS-rebinding protection that the MCP 2025-11-25 specification recommends and that the reference Go SDK has shipped since early 2026. Defaults protect loopback only, so existing setups stay safe; LAN exposure now needs an explicit `PIHOLE_ALLOWED_ORIGINS` extension. Alongside the security work, every mutating tool now validates user-supplied domains, URLs, and free-form strings before any Pi-hole API call. A new `pihole_config_properties` tool surfaces the read-only config keys introduced in Pi-hole FTL v6.6.1 (tool count 73 → 74), and a new `slim` build tag strips OpenTelemetry support to drop the binary ~45% (17 MB → 9 MB stripped) for users who don't run a tracing backend. Both default and slim builds are now shipped as first-class release artefacts — same six platforms, separate tarball and Docker tag families.

### Added

- **HTTP and SSE transport hardening** — two new middlewares wrap the `http` and `sse` handlers (stdio is unaffected):
  - **Rate limiting** — per-session token bucket keyed by `Mcp-Session-Id` (fallback to client IP), default 120 req/min with burst `max(perMinute/4, 30)`. Configure via `PIHOLE_RATE_LIMIT`; `0` disables. Throttled requests return HTTP 429 with `Retry-After: 1`.
  - **Origin and Host validation** — DNS-rebinding protection per the MCP 2025-11-25 spec recommendation. Configure via `PIHOLE_ALLOWED_ORIGINS` (comma-separated). Default `localhost,127.0.0.1,[::1]`; the literal `*` disables (documented as unsafe). Missing `Origin` is allowed for non-browser clients (LibreChat, custom Go clients). Mismatches return HTTP 403.
- **`pihole_config_properties`** — new tool that lists configuration keys locked as read-only by `pihole.toml` or environment variable, with reason and human-readable description. Useful after a `pihole_config_set` rejection to confirm whether a key is intentionally immutable. Requires Pi-hole FTL v6.6.1+; the handler surfaces a friendly fallback error against older releases. Tool count is now **74**.
- **Slim build variant** — `go build -tags slim` (or `just build-slim`) excludes OpenTelemetry support entirely. The slim Linux amd64 binary drops from ~17 MB to ~9 MB stripped (~45% smaller; ~3.5 MB compressed vs ~6 MB). Both default and slim artefacts are now published for every release: tarballs as `pihole-mcp-slim_X.Y.Z_*` and Docker images as `:X.Y.Z-slim` / `:latest-slim`.
- **Input validation** at handler entry for every mutating tool — `pihole_domains_*`, `pihole_lists_*`, `pihole_clients_*`, `pihole_groups_*`, and `pihole_config_*`. Domain names are checked for RFC 1035 compliance (length, labels, no shell metacharacters); list URLs must parse as `http`/`https`/`file` with a non-empty host or path; comments and free-form names are length-capped (1024 / 255 characters). Invalid inputs now return a friendly MCP error before any Pi-hole API call is made, instead of surfacing a raw 400 from the Pi-hole server.
- **`format=csv`** added to `pihole_stats_recent_blocked`, `pihole_stats_query_types`, `pihole_stats_upstreams`, `pihole_stats_database_upstreams`, and `pihole_dhcp_leases`. Total CSV-capable tool count is now 15, saving ~30-40% tokens on large tables.

### Changed

- The `http` and `sse` transports now run inside a `net/http.Server` constructed by `cmd/pihole-mcp/main.go` rather than mcp-go's built-in `.Start()` helper. This is what allows the middleware chain to wrap the MCP handler. Behaviourally identical for clients that respect the existing graceful-shutdown signal handling. A `ReadHeaderTimeout` of 10 seconds is now enforced (mitigates slowloris).
- `pihole_history_graph` / `_history_clients` / `_history_database` / `_history_database_clients` descriptions now lead with the data source ("in-memory" vs "database") and cross-reference each other — removes the cognitive overhead of working out which tool you want from name alone.
- `pihole_network_info` description clarified to point users to `pihole_network_routes` / `pihole_network_interfaces` for richer per-route or per-interface detail.
- `pihole_config_set` is now annotated `openWorldHint: true` — the tool can affect DNS resolution and other services system-wide, and the hint surfaces that to MCP clients that gate destructive operations.

### Fixed

- Hardened `pihole_network_devices` against invalid UTF-8 bytes in the upstream `macVendor` field (Pi-hole FTL upstream issue [#2868](https://github.com/pi-hole/FTL/issues/2868)). Go's `encoding/json` already silently replaces non-UTF-8 sequences with U+FFFD during decode, so this MCP server was unaffected — a regression-prevention test is now in place to lock that behaviour in.

### Quality

- Fixture suite expanded from 13 → 22 captured Pi-hole API responses. `scripts/refresh-fixtures.sh` now also captures the full stats family (`top_domains`, `top_clients`, `upstreams`, `query_types`, `recent_blocked`), the four `stats_database_*` endpoints, and `config_properties` (skipped on older Pi-hole versions that return an empty body for the endpoint).
- New `_RealFixture` shape-validation tests across the stats and auth surfaces. Each runs the handler against the captured response and confirms the handler doesn't crash and emits non-empty output. Hand-written value-assertion mocks remain in place for tests that pin specific numbers.

### Dependencies

- `github.com/mark3labs/mcp-go` bumped 0.47.0 → 0.54.0. Brings panic recovery to the SSE message handler, stdio worker, task goroutines, and session hook goroutines; adds a transport-agnostic `Handle` entry point; adds OpenTelemetry server-side tracing hooks; adds `WithStrictInputSchemaDefault`. No breaking changes for our usage — every `server.NewMCPServer`, `server.NewStreamableHTTPServer`, and `server.NewSSEServer` call site compiles and passes tests unchanged.
- `golang.org/x/time` v0.15.0 added as a direct dependency to back the rate-limit token bucket.

### Migration Notes

- **HTTP and SSE transports now enforce Origin and Host validation by default.** Requests are accepted only when the `Host` (and `Origin`, if present) header resolves to a host in `PIHOLE_ALLOWED_ORIGINS`. The default allowlist covers loopback (`localhost,127.0.0.1,[::1]`) only — if you're exposing pihole-mcp on a LAN or behind a reverse proxy, extend the list:
  ```sh
  export PIHOLE_ALLOWED_ORIGINS="localhost,127.0.0.1,[::1],pihole-mcp.lan"
  ```
  Set `PIHOLE_ALLOWED_ORIGINS=*` to disable the check entirely (only when behind a proxy doing its own access control). stdio is unaffected.
- **Per-session rate limiting is on by default** at 120 req/min with burst 30. If your client legitimately exceeds that during batch refreshes, raise `PIHOLE_RATE_LIMIT` (e.g. `600`) or set `PIHOLE_RATE_LIMIT=0` to disable.
- **Slim build variant is opt-in by archive or tag name** — defaults are unchanged. To pull the lean binary, grab `pihole-mcp-slim_0.4.0_*.tar.gz` from the release assets or the `ghcr.io/hexamatic/pihole-mcp:0.4.0-slim` Docker tag. `OTEL_EXPORTER_OTLP_ENDPOINT` is silently ignored in slim builds.

### Installation

**Go install:**
```
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.4.0
```
For the slim variant (no OpenTelemetry): `go install -tags slim github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.4.0`

**Docker (multi-arch):**
```
docker pull ghcr.io/hexamatic/pihole-mcp:0.4.0           # default (includes OpenTelemetry)
docker pull ghcr.io/hexamatic/pihole-mcp:0.4.0-slim      # slim (~45% smaller, no OTel)
```

**Binary download:** grab the archive for your platform from the release assets — `pihole-mcp_0.4.0_{os}_{arch}.tar.gz` for the default build, `pihole-mcp-slim_0.4.0_{os}_{arch}.tar.gz` for the slim build.

### Requirements

- Pi-hole v6.6+ with the REST API enabled (v6.6.1+ for `pihole_config_properties`)
- An admin password or [application password](https://docs.pi-hole.net/api/auth/)

### Configuration

| Variable                      | Required | Default                       | Description                                                                |
| ----------------------------- | -------- | ----------------------------- | -------------------------------------------------------------------------- |
| `PIHOLE_URL`                  | Yes      | —                             | Pi-hole base URL                                                           |
| `PIHOLE_PASSWORD`             | Yes      | —                             | Admin or application password                                              |
| `PIHOLE_REQUEST_TIMEOUT`      | No       | `30s`                         | HTTP request timeout                                                       |
| `PIHOLE_RATE_LIMIT`           | No       | `120`                         | Per-session requests/min cap on HTTP/SSE transports; `0` disables          |
| `PIHOLE_ALLOWED_ORIGINS`      | No       | `localhost,127.0.0.1,[::1]`   | Origin/Host allowlist for HTTP/SSE transports; `*` disables (unsafe)       |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No       | —                             | OpenTelemetry endpoint (enables tracing; ignored in slim builds)           |

See the [README](https://github.com/hexamatic/pihole-mcp#readme) for client-specific setup guides (Claude Desktop, Cursor, Windsurf, VS Code, Cline) and the [Security section](https://github.com/hexamatic/pihole-mcp#security-http-and-sse-transports) for the transport hardening details.

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

[Unreleased]: https://github.com/hexamatic/pihole-mcp/compare/v0.7.0...HEAD
[v0.7.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.6.0...v0.7.0
[v0.6.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/hexamatic/pihole-mcp/releases/tag/v0.1.0
