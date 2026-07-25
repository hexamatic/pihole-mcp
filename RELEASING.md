# Releasing pihole-mcp

Releases are tag-driven and fully automated. CI validates every push, so by the time a tag exists the build has already passed.

## Cutting a release

1. Confirm `main` is green: `just ci`.
2. Decide the version (semver: `vMAJOR.MINOR.PATCH`).
3. Update `CHANGELOG.md`:
   - Move entries from the `[Unreleased]` section to a new `## [vX.Y.Z] - YYYY-MM-DD` heading.
   - Write a **Highlights** paragraph in prose summarising why this release matters. Aim for the polish of the v0.1.0 release body — that's the bar for every release.
   - Update the reference link list at the bottom (`[Unreleased]` compare URL, new `[vX.Y.Z]` compare URL).
   - Preview exactly what will appear on the GitHub release page:
     ```sh
     just release-notes vX.Y.Z
     ```
   - Commit the changelog: `git commit -m "chore: prepare vX.Y.Z release"`.
4. Tag and push:
   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```
5. The `release.yml` workflow runs goreleaser, which:
   - Extracts the release body from `CHANGELOG.md` via `scripts/release-notes.sh`.
   - Builds 6 binary archives (linux/darwin/windows × amd64/arm64).
   - Builds and pushes the `ghcr.io/hexamatic/pihole-mcp:vX.Y.Z` and `:latest` Docker images (linux/amd64 + linux/arm64).
   - Generates SHA256 checksums.
   - Publishes the GitHub release directly — no manual draft step (see `.goreleaser.yaml` `release.draft: false`).

### Drafting from git log

If `[Unreleased]` is empty or you want a starting point, scaffold a draft entry from the commits since the last tag:

```sh
just changelog-draft vX.Y.Z
```

This groups commits by Conventional Commit prefix into Keep-a-Changelog sections (Added / Changed / Fixed / Dependencies). Pipe to a file or copy into the `[Unreleased]` section, then refine the prose.

### Amending a published release

If a release was already published with poor or incomplete notes, fix `CHANGELOG.md` for that version and push the new body up without rebuilding artefacts:

```sh
just release-notes vX.Y.Z > /tmp/notes.md
gh release edit vX.Y.Z --repo hexamatic/pihole-mcp --notes-file /tmp/notes.md
```

This only updates the release body — the tag, binaries, Docker images, and SHA256SUMS remain untouched.

## Verifying a release

After the workflow completes:

- The release appears on https://github.com/hexamatic/pihole-mcp/releases as published (not draft).
- `docker pull ghcr.io/hexamatic/pihole-mcp:vX.Y.Z` succeeds.
- The binary downloaded from the release archive prints the right version: `./pihole-mcp -version`.
- The MCP Registry listing reflects the new version:
  ```sh
  curl -s 'https://registry.modelcontextprotocol.io/v0/servers?search=io.github.hexamatic/pihole-mcp' | jq '.servers[0].server.version'
  ```

## MCP Registry publishing

The `publish-mcp` job in `.github/workflows/release.yml` publishes `server.json` to the
[official MCP Registry](https://registry.modelcontextprotocol.io/) after the `release` job
finishes. It runs as a separate job deliberately — the registry is in preview, and a rejection
there must not mark an otherwise good release as failed.

How it works:

- `server.json` is committed carrying the **previous** release's version, so the drift tests in
  `internal/config/serverjson_test.go` can assert `.version`, `.packages[0].version` and the tag
  in `.packages[0].identifier` all agree. The job rewrites all three from the git tag with `jq`;
  there is nothing to bump by hand before tagging.
- Ownership is proved by the `io.modelcontextprotocol.server.name` label on the published GHCR
  image, which the registry reads and matches against `name` in `server.json`. Both live in
  `Dockerfile.goreleaser`, and the drift test fails the build if they diverge.
- Authentication is GitHub Actions OIDC (`mcp-publisher login github-oidc`), which grants the
  `io.github.hexamatic/*` namespace. No token or secret is required.

If the job fails, the release itself is already complete and signed. Fix the cause and re-run
just that job:

```sh
gh run rerun <run-id> --job <job-id>
```

The image must be public for the registry to inspect it anonymously — verify with
`gh api orgs/hexamatic/packages/container/pihole-mcp --jq .visibility`.

## Local dry-run

Before tagging, verify the release pipeline parses cleanly:
```sh
just release-dry
```
This produces a snapshot under `dist/` without uploading anything. Inspect the manifest if anything has changed in `.goreleaser.yaml`. The dry run skips signing and SBOM generation (`--skip=sign,sbom`): keyless cosign needs the CI OIDC identity and would open a browser locally. Those steps only run — and can only be verified — in the tag-triggered workflow; after the release publishes, run the verification commands in [SECURITY.md](SECURITY.md#verifying-release-artefacts) against the live artefacts.

## Homebrew tap

The `homebrew_casks:` block in `.goreleaser.yaml` targets a separate `hexamatic/homebrew-tap` repository, writing `Casks/pihole-mcp.rb`. The tap repo must exist and be writable by `TAP_GITHUB_TOKEN` before the first release that publishes to it. If the tap is not yet configured, goreleaser logs a warning but does not fail the release — binaries and Docker images publish as normal.

This was a `brews:` (formula) block until v0.8.0. goreleaser deprecated formulae in v2.10, and the generated cask covers both macOS and Linux because its only artefact is a portable `binary` stanza. Two one-off steps go with the migration, in this order:

1. **Before** the first cask release: add `tap_migrations.json` to the tap root containing `{"pihole-mcp": "pihole-mcp"}`. This is what makes `brew upgrade` move an existing user from the formula to the cask rather than erroring.
2. **After** the first cask release has landed and `brew install hexamatic/tap/pihole-mcp` has been confirmed working: delete the legacy formula at the tap root (`pihole-mcp.rb`). Deleting it earlier leaves a window where the tap offers neither.

## Rolling back

GitHub releases can be deleted from the web UI; the underlying tag remains. To release a new version that supersedes a botched one, increment the patch number rather than re-tagging the same version (Docker images and Homebrew formulas are immutable per tag).
