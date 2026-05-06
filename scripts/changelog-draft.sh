#!/usr/bin/env sh
# Scaffold a CHANGELOG.md entry for the next release from git log.
#
# Reads commits from the previous tag to HEAD, groups them by Conventional
# Commit prefix into Keep-a-Changelog subsections, and prints a draft entry
# to stdout. The output is a starting point — refine the prose, drop noise,
# add a Highlights paragraph before pasting into CHANGELOG.md.
#
# Usage: scripts/changelog-draft.sh vX.Y.Z [previous-tag]
# Defaults: previous-tag = `git describe --tags --abbrev=0`

set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    printf 'Usage: %s vX.Y.Z [previous-tag]\n' "$0" >&2
    exit 2
fi

VERSION="$1"
PREV="${2:-}"

if [ -z "$PREV" ]; then
    PREV="$(git describe --tags --abbrev=0 2>/dev/null || true)"
fi

if [ -z "$PREV" ]; then
    RANGE="HEAD"
    range_display="all commits"
else
    RANGE="${PREV}..HEAD"
    range_display="$PREV..HEAD"
fi

DATE_TODAY="$(date -u +%Y-%m-%d)"

cat <<EOF
## [${VERSION}] - ${DATE_TODAY}

### Highlights

TODO — write a short prose paragraph describing why this release matters.
This is the first thing users see on the GitHub release page; aim for the
polish of the v0.1.0 release body.

EOF

# Categorise commits in $range_display by Conventional Commit prefix.
git log --pretty=format:'%s|%h' "$RANGE" | awk -F'|' '
    {
        subject = $1
        sha = $2
        if (match(subject, /^feat(\([^)]+\))?!?:[[:space:]]*/)) {
            added[++n_added] = substr(subject, RSTART + RLENGTH) " (" sha ")"
        } else if (match(subject, /^(perf|refactor)(\([^)]+\))?!?:[[:space:]]*/)) {
            changed[++n_changed] = substr(subject, RSTART + RLENGTH) " (" sha ")"
        } else if (match(subject, /^fix(\([^)]+\))?!?:[[:space:]]*/)) {
            fixed[++n_fixed] = substr(subject, RSTART + RLENGTH) " (" sha ")"
        } else if (match(subject, /^(build|deps|dep)(\([^)]+\))?!?:[[:space:]]*/)) {
            deps[++n_deps] = substr(subject, RSTART + RLENGTH) " (" sha ")"
        } else if (match(subject, /^(docs|test|ci|chore|style|revert)(\([^)]+\))?!?:[[:space:]]*/)) {
            # Internal-only — drop from user-facing changelog.
            next
        } else {
            other[++n_other] = subject " (" sha ")"
        }
    }
    END {
        if (n_added > 0) {
            print "### Added"; print ""
            for (i = 1; i <= n_added; i++) print "- " added[i]
            print ""
        }
        if (n_changed > 0) {
            print "### Changed"; print ""
            for (i = 1; i <= n_changed; i++) print "- " changed[i]
            print ""
        }
        if (n_fixed > 0) {
            print "### Fixed"; print ""
            for (i = 1; i <= n_fixed; i++) print "- " fixed[i]
            print ""
        }
        if (n_deps > 0) {
            print "### Dependencies"; print ""
            for (i = 1; i <= n_deps; i++) print "- " deps[i]
            print ""
        }
        if (n_other > 0) {
            print "### Other (review and re-categorise — these did not match a conventional-commit prefix)"
            print ""
            for (i = 1; i <= n_other; i++) print "- " other[i]
            print ""
        }
    }
'

cat <<EOF
---
Scaffolded from ${range_display}. Refine the bullets, write the Highlights
paragraph, drop noise, then move this block into CHANGELOG.md under the
[Unreleased] section (or rename the heading to [${VERSION}] - ${DATE_TODAY}
if releasing now).
EOF
