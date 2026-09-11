# Releasing pihole-mcp

Releases are tag-driven and fully automated. CI validates every push, so by the time a tag exists the build has already passed.

## Cutting a release

1. Confirm `main` is green: `just ci`.
2. Decide the version (semver: `vMAJOR.MINOR.PATCH`).
3. Update `CHANGELOG.md`:
   - Move entries from the `[Unreleased]` section to a new `## [vX.Y.Z] - YYYY-MM-DD` heading.
   - Write a **Highlights** paragraph in prose summarising why this release matters. Aim for the polish of the v0.1.0 release body: that's the bar for every release.
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
   - Builds and pushes the `ghcr.io/hexamatic/pihole-mcp:X.Y.Z` and `:latest` Docker images (linux/amd64 + linux/arm64).
   - Generates SHA256 checksums.
   - Publishes the GitHub release directly, with no manual draft step (see `.goreleaser.yaml` `release.draft: false`).

   The workflow then attests build provenance for the archives and for both container images, and
   uploads the attestation bundle a second time as `pihole-mcp_X.Y.Z_SHA256SUMS.intoto.jsonl`. That
   duplicate asset is purely for OpenSSF Scorecard, whose Signed-Releases probe suffix-matches that
   extension and nothing else; the provenance that actually verifies is the one
   `gh attestation verify` resolves. Those steps run on `always()` and gate themselves on the
   artefacts existing, so a late failure in the Homebrew or Scoop publisher cannot leave a published
   release without provenance.

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

This only updates the release body. The tag, binaries, Docker images, and SHA256SUMS remain untouched.

## Verifying a release

After the workflow completes:

- The release appears on https://github.com/hexamatic/pihole-mcp/releases as published (not draft).
- `docker pull ghcr.io/hexamatic/pihole-mcp:X.Y.Z` succeeds.
- The binary downloaded from the release archive prints the right version: `./pihole-mcp -version`.
- Provenance resolves for both the archives and the images:
  ```sh
  gh attestation verify pihole-mcp_X.Y.Z_linux_amd64.tar.gz --repo hexamatic/pihole-mcp
  gh attestation verify oci://ghcr.io/hexamatic/pihole-mcp:X.Y.Z --repo hexamatic/pihole-mcp
  ```
- The release carries a `pihole-mcp_X.Y.Z_SHA256SUMS.intoto.jsonl` asset (Scorecard's Signed-Releases
  probe looks for that suffix and nothing else).
- The MCP Registry listing reflects the new version. The search endpoint returns every version ever
  published, oldest first, so take the entry the registry itself marks current rather than
  `.servers[0]`, which grabbed v0.8.0 out of a response that also contained v0.8.1:
  ```sh
  curl -s 'https://registry.modelcontextprotocol.io/v0/servers?search=io.github.hexamatic/pihole-mcp' \
    | jq '.servers[] | select(._meta["io.modelcontextprotocol.registry/official"].isLatest) | .server.version'
  ```

## MCP Registry publishing

`.github/workflows/publish-mcp.yml` publishes `server.json` to the
[official MCP Registry](https://registry.modelcontextprotocol.io/). It triggers automatically on a
successful tag-driven `Release` run, and can also be dispatched by hand.

How it works:

- `server.json` is committed carrying the **previous** release's version, so the drift tests in
  `internal/config/serverjson_test.go` can assert `.version` and the tag inside
  `.packages[0].identifier` agree, and that `.packages[0].version` is **absent**: the registry
  rejects a `version` field on an OCI package. The workflow rewrites `.version` and the identifier
  tag with `jq` and deletes `.packages[0].version` outright rather than setting it; there is
  nothing to bump by hand before tagging.
- Ownership is proved by the `io.modelcontextprotocol.server.name` label on the published GHCR
  image, which the registry reads and matches against `name` in `server.json`. The published image
  is built from `Dockerfile.goreleaser`; the root `Dockerfile` carries the same label, and
  `TestServerJSONNameMatchesImageLabel` now reads **both** and fails the build if either diverges.
- `.github/workflows/mcp-validate.yml` runs `mcp-publisher validate` against the live registry on
  every pull request touching `server.json`, and weekly. It needs no authentication and publishes
  nothing. It exists because the registry enforces rules the published JSON Schema does not, and two
  of v0.8.0's four follow-ups were registry-side rejections found only after a tag had gone out.
- Authentication is GitHub Actions OIDC (`mcp-publisher login github-oidc`), which grants the
  `io.github.hexamatic/*` namespace. No token or secret is required.
- The image must be public for the registry to inspect it anonymously. Verify with
  `gh api orgs/hexamatic/packages/container/pihole-mcp --jq .visibility`.

**It checks out the default branch, not the tag, and takes the version as an input.** That is
deliberate. v0.8.0's first publish attempt was rejected with a 422 for a 138-character
`description` against a documented 100-character cap. Had the publish lived inside `release.yml`,
correcting it would have meant moving the tag, which is not acceptable, because the release's
cosign signatures and SLSA attestations reference the exact commit that was built, so moving the
tag breaks the provenance chain the release exists to provide.

So fixing listing metadata is a normal pull request followed by:

```sh
gh workflow run publish-mcp.yml -f version=X.Y.Z
```

Publishing the same version again is an update, not an error. A failed publish never affects the
release, which is already complete, signed and attested by the time this runs.

## Rehearsing the release path

`.github/workflows/release-rehearsal.yml` runs the release pipeline short of publishing. It fires on
every pull request touching `.goreleaser.yaml`, `release.yml`, `Dockerfile.goreleaser`,
`scripts/release-notes.sh` or `CHANGELOG.md`, monthly on a schedule, and on demand:

```sh
gh workflow run release-rehearsal.yml --repo hexamatic/pihole-mcp
```

It exists because v0.8.0's first tag failed on a cosign v3 breaking change in a signing block that
no run had ever executed. `--snapshot` suppresses only the Publish, Announce and Validate stages, so
everything below them runs for real: both builds, all twelve archives, nfpm's deb and rpm packages,
syft's SBOMs, and cosign's keyless signature over the checksum file. The workflow then asserts each
of those artefacts is present rather than trusting goreleaser's exit code, because several of these
pipes skip quietly when a precondition is unmet.

**What the rehearsal still cannot cover.** Everything inside goreleaser's `publish` stage runs only
on a tag, and this is the honest list:

| Tag-only | What could still break there |
|---|---|
| `dockerv2.Publish` | The multi-arch push to GHCR. A snapshot loads one image per platform locally instead. |
| `sign.DockerPipe` (`docker_signs`) | Image signatures, which need a pushed digest to sign. |
| Homebrew cask and Scoop publishing | The commit into `hexamatic/homebrew-tap` and `hexamatic/scoop-bucket`. The manifests are *generated* in the rehearsal; only the push is untested. |
| `release.Pipe` | Creating the GitHub release and uploading assets to it. |

The image digest count is the one number that differs between the two: a real release publishes two
multi-arch manifests and therefore two distinct digests, which is what `release.yml`'s attestation
steps expect, while a snapshot yields one per platform. The rehearsal reports the count rather than
asserting it, and `release.yml` annotates rather than fails if it is not two, so an unattested image
never costs a release.

## Local dry-run

For a fast local check that the config parses and builds:
```sh
just release-check   # goreleaser check, no build
just release-dry     # snapshot into dist/, uploads nothing
```
`release-dry` passes `--skip=sign,sbom`. That is a *local* workaround, not a property of snapshot
mode: keyless cosign wants the CI OIDC identity and would open a browser here, and syft may not be
installed. Both pipes do run under a bare `--snapshot`, which is exactly what the rehearsal workflow
exercises. **If you are changing the signing or SBOM blocks, `just release-dry` will not tell you
whether they work.** Open a pull request and read the rehearsal. After the release publishes, run
the verification commands in [SECURITY.md](SECURITY.md#verifying-release-artefacts) against the live
artefacts.

## Homebrew tap

The `homebrew_casks:` block in `.goreleaser.yaml` targets a separate `hexamatic/homebrew-tap` repository, writing `Casks/pihole-mcp.rb`. The tap repo must exist and be writable by `TAP_GITHUB_TOKEN` before the first release that publishes to it. If the tap is not yet configured, goreleaser logs a warning but does not fail the release: binaries and Docker images publish as normal.

This was a `brews:` (formula) block until v0.8.0. goreleaser deprecated formulae in v2.10, and the generated cask covers both macOS and Linux because its only artefact is a portable `binary` stanza. Two one-off steps go with the migration, in this order:

1. **Before** the first cask release: add `tap_migrations.json` to the tap root containing `{"pihole-mcp": "pihole-mcp"}`. This is what makes `brew upgrade` move an existing user from the formula to the cask rather than erroring.
2. **After** the first cask release has landed and `brew install hexamatic/tap/pihole-mcp` has been confirmed working: delete the legacy formula at the tap root (`pihole-mcp.rb`). Deleting it earlier leaves a window where the tap offers neither.

## Rolling back

GitHub releases can be deleted from the web UI; the underlying tag remains. To release a new version that supersedes a botched one, increment the patch number rather than re-tagging the same version (Docker images and Homebrew formulas are immutable per tag).
