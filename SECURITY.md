# Security Policy

## Reporting Security Vulnerabilities

If you discover a security vulnerability in this MCP server, please report it **privately** using [GitHub Security Advisories](https://github.com/hexamatic/pihole-mcp/security/advisories/new) rather than the public issue tracker.

Please include:

- Type of vulnerability (e.g. credential exposure, injection, authentication bypass)
- Location in source code (file path and line number if possible)
- Steps to reproduce
- Potential impact (especially regarding Pi-hole API credential handling)
- Suggested fix (if any)

## Response Timeline

- **Acknowledgement:** within 48 hours
- **Assessment:** severity and impact evaluated within 1 week
- **Fix:** developed in a private branch
- **Release:** patch version published with security advisory

## Security Considerations

This MCP server handles Pi-hole API credentials. Users should:

- **Use environment variables** for `PIHOLE_URL` and `PIHOLE_PASSWORD`. Never hardcode credentials.
- **Use application passwords** instead of the main admin password where possible (can be revoked independently)
- **Use HTTPS** when connecting to Pi-hole over untrusted networks
- **Restrict network access** to the Pi-hole instance using firewall rules
- **Keep dependencies updated.** Enable Dependabot or run `go get -u` regularly.

### HTTP and SSE transports

The default transport is stdio, which is a single-process, single-user channel and is not exposed
on the network. The optional `http` and `sse` transports are, and they have **no authentication
until one is configured**:

- **Set `PIHOLE_HTTP_AUTH_TOKEN` (or `PIHOLE_HTTP_AUTH_TOKEN_FILE`) whenever the listen address is
  not loopback.** Without it, anyone who can reach the port can disable blocking, rewrite
  blocklists and read query history. The server logs an error at startup in that state, and starts
  anyway so that a reverse proxy holding the authentication remains a valid deployment.
- **Rejected requests are throttled per address**, on a budget separate from `PIHOLE_RATE_LIMIT`
  and active even when that is set to `0`, so a shared token cannot be guessed at line rate. The
  server logs the throttling, at most once a minute per address.
- **Do not treat `PIHOLE_ALLOWED_ORIGINS` as access control.** Origin and Host validation is
  DNS-rebinding protection for browsers. Both headers are supplied by the caller, so any
  non-browser client sets them to whatever the allowlist accepts.
- **Prefer the file form of the token.** A value passed as an environment variable on a command
  line is readable via `ps` by every user on the host.
- **Set `PIHOLE_TRUSTED_PROXIES` only for proxies you control.** It is what makes the rate limiter
  believe `X-Forwarded-For`, and that header is client-supplied.

## Data Handling

pihole-mcp talks to two parties only: the Pi-hole instances you configure, and the MCP client that started it. It has no telemetry, update check or crash reporting. The one optional third destination is an OpenTelemetry collector, used only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set, and its spans carry the tool name, timing and any error message. They never carry tool arguments or results, although an error message can quote the value that caused it. The slim build contains no OpenTelemetry code at all.

### What the MCP client receives

Tool results go to the MCP client, and so to whatever model that client is using. If that model is hosted, its provider's data handling terms apply to everything below. Several tools return personal information about the people and devices on your network:

- **DNS query history tied to the device that asked**, meaning the domain, the client's address and hostname, and the time: `pihole_queries_search`, and the raw resolver log from `pihole_logs_dnsmasq`.
- **An inventory of your network**: MAC addresses, hostnames, addresses and hardware vendors from `pihole_network_devices`, and DHCP leases from `pihole_dhcp_leases`.
- **Activity per client**, including top clients by address, from the history and stats tools and from `pihole_padd`.
- **Other people's Pi-hole sessions**, including the address and browser user agent of anyone logged in to the web interface or another integration, from `pihole_auth_sessions`.

Password hashes are the exception. Pi-hole's configuration API masks the admin password and the TOTP secret but returns `webserver.api.pwhash` and `webserver.api.app_pwhash` in full. From v0.9.0 the configuration tools withhold both from everything they return, including the configuration `pihole_config_set` echoes after a write, and refuse to write either, because Pi-hole accepts a written hash and it replaces the credential outright. Earlier versions returned the admin password's hash.

### Limiting it

`PIHOLE_READ_ONLY` removes the tools that change Pi-hole. It does not hide any of the information above, because every tool listed is a read.

To keep a category away from the client, leave its toolset out of `PIHOLE_TOOLSETS`:

| To withhold | Leave out |
|---|---|
| Query history | `queries`, `history`, `logs` |
| Device inventory and DHCP leases | `network`, `dhcp`, `clients` |
| Other users' sessions | `sessions` |
| Backup archives (see below) | `teleporter`, `instance` |

`stats` and `dashboard` also name top clients by address, so leave those out as well if no client address should reach the model.

### Backups written to disk

`pihole_teleporter_export` saves a Pi-hole backup archive to disk, and so does `pihole_instance_sync` just before it applies a change, unless it is called with `snapshot` set to false. Only the file's path and size reach the MCP client, but the archive holds the password hash, every DHCP lease and the long-term query database. Archives are created readable by their owner only, whether in the temp directory or at an `output_path` you choose. They are not deleted when written: files in the temp directory older than 24 hours are removed the next time either tool runs, and anything written to an `output_path` is yours to delete.

### Logs

The server's own log output never contains the Pi-hole password, the session ID, or tool arguments and results. It records its settings at startup and, when the rate limiter throttles a client, that client's address at most once a minute.

## Verifying Release Artefacts

Every release from v0.8.0 onwards ships with checksums, keyless [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) signatures, SPDX SBOMs, and SLSA build provenance. To verify a download:

**Checksums**

```bash
sha256sum -c pihole-mcp_X.Y.Z_SHA256SUMS --ignore-missing
```

**Checksum-file signature** (proves the checksums were produced by this repository's release workflow). The signature, certificate and inclusion proof all travel in a single Sigstore bundle, `pihole-mcp_X.Y.Z_SHA256SUMS.sigstore.json`:

```bash
cosign verify-blob \
  --bundle pihole-mcp_X.Y.Z_SHA256SUMS.sigstore.json \
  --certificate-identity-regexp='^https://github\.com/hexamatic/pihole-mcp' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  pihole-mcp_X.Y.Z_SHA256SUMS
```

Requires cosign v3 or newer. On cosign v2, pass `--new-bundle-format` as well.

**Docker images**

```bash
cosign verify \
  --certificate-identity-regexp='^https://github\.com/hexamatic/pihole-mcp' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  ghcr.io/hexamatic/pihole-mcp:X.Y.Z
```

**Build provenance** (requires the [GitHub CLI](https://cli.github.com/)):

```bash
gh attestation verify pihole-mcp_X.Y.Z_linux_amd64.tar.gz --repo hexamatic/pihole-mcp
```

From v0.9.0 the container images carry build provenance as well, pushed to the registry alongside the image so it resolves without a download:

```bash
gh attestation verify oci://ghcr.io/hexamatic/pihole-mcp:X.Y.Z --repo hexamatic/pihole-mcp
gh attestation verify oci://ghcr.io/hexamatic/pihole-mcp:X.Y.Z-slim --repo hexamatic/pihole-mcp
```

This is a different guarantee from the `cosign verify` above and worth running as well as, not instead of. The cosign signature proves the image was signed by this repository's release workflow; the attestation is a signed SLSA statement of *how* it was built, naming the workflow, the commit and the runner.

**SBOMs.** Each archive has a matching `.sbom.json` (SPDX) release asset listing the exact dependency versions compiled into that binary.

## Scope

The following are in scope for security reports:

- Credential leakage (Pi-hole passwords exposed in logs, errors, or responses)
- Authentication bypass in the session management logic
- Bypass of the HTTP/SSE bearer token, Origin/Host validation, or rate limiting
- Injection vulnerabilities in API request construction
- Denial of service through resource exhaustion

Out of scope:

- Vulnerabilities in Pi-hole itself (report to [Pi-hole](https://github.com/pi-hole/FTL/security))
- Vulnerabilities in the mcp-go SDK (report to [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go))
