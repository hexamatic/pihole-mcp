#!/usr/bin/env sh
# Extract a version's body from CHANGELOG.md and print to stdout.
#
# Used by .github/workflows/release.yml to feed goreleaser --release-notes.
# Also exposed as `just release-notes vX.Y.Z` for local preview.
#
# Usage: scripts/release-notes.sh vX.Y.Z [path/to/CHANGELOG.md]
# Exits non-zero (and prints to stderr) if no matching section is found.

set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    printf 'Usage: %s vX.Y.Z [CHANGELOG.md]\n' "$0" >&2
    exit 2
fi

VERSION="$1"
CHANGELOG="${2:-CHANGELOG.md}"

if [ ! -f "$CHANGELOG" ]; then
    printf 'release-notes: %s not found\n' "$CHANGELOG" >&2
    exit 1
fi

output=$(
    awk -v ver="$VERSION" '
        # Stop at the next ## [...] heading once we have entered the section.
        found && /^## \[/ { exit }
        # Match the target heading; mark found and skip the heading itself.
        /^## \[/ && $0 ~ "^## \\[" ver "\\]" { found = 1; next }
        found {
            # Drop reference-style link defs ([v0.2.0]: https://...) — they
            # belong to the bottom of the file, not the release body.
            if ($0 ~ /^\[[^]]+\]:[[:space:]]*http/) next
            # Strip the leading blank line that sits between the heading
            # and the first content line.
            if (!started) {
                if ($0 ~ /^[[:space:]]*$/) next
                started = 1
            }
            lines[++n] = $0
            if ($0 ~ /[^[:space:]]/) last = n
        }
        # Trim trailing blank lines on the way out.
        END { for (i = 1; i <= last; i++) print lines[i] }
    ' "$CHANGELOG"
)

if [ -z "$output" ]; then
    printf 'release-notes: no entry for %s in %s\n' "$VERSION" "$CHANGELOG" >&2
    exit 1
fi

printf '%s\n' "$output"
