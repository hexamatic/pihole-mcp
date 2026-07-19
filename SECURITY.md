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

## Verifying Release Artefacts

Every release from v0.8.0 onwards ships with checksums, keyless [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) signatures, SPDX SBOMs, and SLSA build provenance. To verify a download:

**Checksums**

```bash
sha256sum -c pihole-mcp_X.Y.Z_SHA256SUMS --ignore-missing
```

**Checksum-file signature** (proves the checksums were produced by this repository's release workflow):

```bash
cosign verify-blob \
  --certificate pihole-mcp_X.Y.Z_SHA256SUMS.pem \
  --signature   pihole-mcp_X.Y.Z_SHA256SUMS.sig \
  --certificate-identity-regexp='^https://github\.com/hexamatic/pihole-mcp' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  pihole-mcp_X.Y.Z_SHA256SUMS
```

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

**SBOMs** — each archive has a matching `.sbom.json` (SPDX) release asset listing the exact dependency versions compiled into that binary.

## Scope

The following are in scope for security reports:

- Credential leakage (Pi-hole passwords exposed in logs, errors, or responses)
- Authentication bypass in the session management logic
- Injection vulnerabilities in API request construction
- Denial of service through resource exhaustion

Out of scope:

- Vulnerabilities in Pi-hole itself (report to [Pi-hole](https://github.com/pi-hole/FTL/security))
- Vulnerabilities in the mcp-go SDK (report to [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go))
