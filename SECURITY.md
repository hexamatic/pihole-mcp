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

- **Use environment variables** for `PIHOLE_URL` and `PIHOLE_PASSWORD` — never hardcode credentials
- **Use application passwords** instead of the main admin password where possible (can be revoked independently)
- **Use HTTPS** when connecting to Pi-hole over untrusted networks
- **Restrict network access** to the Pi-hole instance using firewall rules
- **Keep dependencies updated** — enable Dependabot or run `go get -u` regularly

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

**SBOMs** — each archive has a matching `.sbom.json` (SPDX) release asset listing the exact dependency versions compiled into that binary.

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
