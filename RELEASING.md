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

## Local dry-run

Before tagging, verify the release pipeline parses cleanly:
```sh
just release-dry
```
This produces a snapshot under `dist/` without uploading anything. Inspect the manifest if anything has changed in `.goreleaser.yaml`.

## Homebrew tap (when configured)

The `brews:` block in `.goreleaser.yaml` (added in v0.3.0) targets a separate `hexamatic/homebrew-tap` repository. The tap repo must exist and be writable by the GitHub Actions token before the first release that publishes to it. If the tap is not yet configured, goreleaser logs a warning but does not fail the release — binaries and Docker images publish as normal.

## Rolling back

GitHub releases can be deleted from the web UI; the underlying tag remains. To release a new version that supersedes a botched one, increment the patch number rather than re-tagging the same version (Docker images and Homebrew formulas are immutable per tag).
